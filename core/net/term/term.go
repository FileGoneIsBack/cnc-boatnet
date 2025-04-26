package term

import (
    "bufio"
    "bytes"
    "io"
    "log"
    "net"
    "os"
    "fmt"
    "sync"
    "strings"
    "golang.org/x/term"
)

const (
    esc            = 0x1b
    // minimum typed letters before showing ghost completion
    minCompleteLen = 2
)

type Completer func(prefix string) []string

type Term struct {
    r         *bufio.Reader
    w         io.Writer
    fd        int
    oldState  *term.State
    History   []string
    HistIndex int
    completer Completer
    mu        sync.Mutex
    prevSuggestCount int
}

// New wraps any io.ReadWriter. If completer is non-nil it will be used for tab-completion.
// If rw is an *os.File, we put it into raw mode. If it's a net.Conn we send
// the minimal Telnet ECHO/SUPPRESS-GAO negotiation; SSHConn is skipped.
func New(rw io.ReadWriter, completer Completer) *Term {
    reader := bufio.NewReader(rw)
    writer := io.Writer(rw)
    var (
        fd       int
        oldState *term.State
    )

    if _, ok := rw.(*SSHConn); ok {
    } else if f, ok := rw.(*os.File); ok {
        fd = int(f.Fd())
        state, err := term.MakeRaw(fd)
        if err != nil {
            log.Fatalf("term: cannot make raw: %v", err)
        }
        oldState = state
        reader = bufio.NewReader(f)
        writer = f
    } else if conn, ok := rw.(net.Conn); ok {
        conn.Write([]byte{255, 251, 1}) // IAC WILL ECHO
        conn.Write([]byte{255, 251, 3}) // IAC WILL SUPPRESS GO AHEAD
    }

    t := &Term{
        r:         reader,
        w:         writer,
        fd:        fd,
        oldState:  oldState,
        History:   make([]string, 0),
        completer: completer,
    }
    if t.completer == nil {
        t.completer = func(string) []string { return nil }
    }
    return t
}


func (t *Term) Close() {
    if t.oldState != nil {
        term.Restore(t.fd, t.oldState)
    }
}


func (t *Term) ReadLine(prompt string) (string, error) {
    t.mu.Lock()
    defer t.mu.Unlock()

    var buf bytes.Buffer
    t.HistIndex = len(t.History)
    idx := 0

    t.w.Write([]byte(prompt))

    for {
        b, err := t.r.ReadByte()
        if err != nil {
            return "", err
        }

        switch b {
        case esc:
            seq, err := t.r.Peek(2)
            if err != nil {
                continue
            }
            t.r.Discard(2)
            key := string(seq)
            switch key {
            case "[A": // up
                if t.HistIndex > 0 {
                    t.HistIndex--
                    line := t.History[t.HistIndex]
                    t.w.Write([]byte("\r\033[2K" + prompt + line))
                    buf.Reset()
                    buf.WriteString(line)
                    idx = len(line)
                }
            case "[B": // down
                if t.HistIndex < len(t.History)-1 {
                    t.HistIndex++
                    line := t.History[t.HistIndex]
                    t.w.Write([]byte("\r\033[2K" + prompt + line))
                    buf.Reset()
                    buf.WriteString(line)
                    idx = len(line)
                } else {
                    t.HistIndex = len(t.History)
                    t.w.Write([]byte("\r\033[2K" + prompt))
                    buf.Reset()
                    idx = 0
                }
            case "[D": // left
                if idx > 0 {
                    idx--
                    t.w.Write([]byte("\x1b[1D"))
                }
            case "[C": // right
                if idx < buf.Len() {
                    t.w.Write([]byte("\x1b[1C"))
                    idx++
                }
            }

        case '\r':
            t.w.Write([]byte("\r\n"))
            line := buf.String()
            if line != "" {
                t.History = append(t.History, line)
            }
            return line, nil

        case '\n':
            continue

        case 127: // backspace
            if idx > 0 {
                data := buf.Bytes()
                t.w.Write([]byte("\b \b"))
                before := data[:idx-1]
                after  := data[idx:]
                buf.Reset()
                buf.Write(before)
                buf.Write(after)
                idx--
        
                t.w.Write([]byte("\033[K"))
        
                if len(after) > 0 {
                    t.w.Write(after)
                }
        
                if len(after) > 0 {
                    t.w.Write([]byte(fmt.Sprintf("\033[%dD", len(after))))
                }
        
                if t.prevSuggestCount > 0 {
                    t.w.Write([]byte("\0337")) // save
                    for i := 0; i < t.prevSuggestCount; i++ {
                        t.w.Write([]byte("\r\n\033[K"))
                    }
                    t.w.Write([]byte("\0338")) // restore
                    t.prevSuggestCount = 0
                }
        
                prefix := buf.String()
                if len(prefix) >= minCompleteLen {
                    matches := t.completer(prefix)
        
                    switch len(matches) {
                    case 0:
                    case 1:
                        sug := matches[0]
                        if len(sug) > len(prefix) {
                            comp := sug[len(prefix):]
                            t.w.Write([]byte("\0337"))           // save
                            t.w.Write([]byte("\033[90m" + comp)) // greyed
                            t.w.Write([]byte("\0338"))           // restore
                        }
                    default:
                        t.w.Write([]byte("\0337")) // save
                        indent := strings.Repeat(" ", len(prompt)+len(prefix))
                        for _, m := range matches {
                            t.w.Write([]byte("\r\n" + indent + m))
                        }
                        t.w.Write([]byte("\0338")) // restore
                        t.prevSuggestCount = len(matches)
        
                        first := matches[0]
                        if len(first) > len(prefix) {
                            comp := first[len(prefix):]
                            t.w.Write([]byte("\0337"))
                            t.w.Write([]byte("\033[90m" + comp))
                            t.w.Write([]byte("\0338"))
                        }
                    }
                }
            }
        case '\t':
            full := buf.String()
            matches := t.completer(full)
        
            if len(matches) == 0 {
                t.w.Write([]byte("\a"))
                break
            }
            if len(matches) == 1 {
                sug := matches[0]
                lastToken := full
                if i := strings.LastIndex(full, " "); i >= 0 {
                    lastToken = full[i+1:]
                }
                if len(sug) > len(lastToken) {
                    delta := sug[len(lastToken):]
                    t.w.Write([]byte(delta))
                    buf.WriteString(delta)
                    idx += len(delta)
                }
            } else {
                t.selectCompletion(prompt, full, matches, &buf, &idx)
            } 
        default:
            t.w.Write([]byte{b})
            buf.WriteByte(b)
            idx++
            t.w.Write([]byte("\033[K")) 
        
            prefix := buf.String()
            if len(prefix) >= minCompleteLen {
                matches := t.completer(prefix)
        

                if t.prevSuggestCount > 0 {
                    t.w.Write([]byte("\0337"))        // save cursor
                    for i := 0; i < t.prevSuggestCount; i++ {
                        t.w.Write([]byte("\r\n\033[K"))  // new line + clear
                    }
                    t.w.Write([]byte("\0338"))        // restore
                    t.prevSuggestCount = 0
                }
        
                switch len(matches) {
                case 0:
                case 1:

                    sug := matches[0]
                    if len(sug) > len(prefix) {
                        comp := sug[len(prefix):]
                        t.w.Write([]byte("\0337"))          // save cursor
                        t.w.Write([]byte("\033[90m" + comp))// greyed
                        t.w.Write([]byte("\0338"))          // restore
                    }
                default:
                    t.w.Write([]byte("\0337")) // save cursor
                    indent := strings.Repeat(" ", len(prompt)+len(prefix))
                    for _, m := range matches {
                        t.w.Write([]byte("\r\n" + indent + m))
                    }
                    t.w.Write([]byte("\0338")) // restore
                    t.prevSuggestCount = len(matches)
        
                    first := matches[0]
                    if len(first) > len(prefix) {
                        comp := first[len(prefix):]
                        t.w.Write([]byte("\0337"))
                        t.w.Write([]byte("\033[90m" + comp))
                        t.w.Write([]byte("\0338"))
                    }
                }
            }
        }
    }
}

func (t *Term) selectCompletion(prompt, prefix string, matches []string, buf *bytes.Buffer, idx *int) {
    drawDropdown := func(sel int) {
        t.w.Write([]byte("\0337")) // save cursor
        indent := strings.Repeat(" ", len(prompt)+len(prefix))
        for _, m := range matches {
            t.w.Write([]byte("\r\n" + indent))
            if m == matches[sel] {
                t.w.Write([]byte("\033[7m" + m + "\033[0m")) // inverse video
            } else {
                t.w.Write([]byte(m))
            }
        }
        t.w.Write([]byte("\0338")) // restore cursor
    }

    sel := 0
    drawDropdown(sel)

    for {
        b, _ := t.r.ReadByte()
        if b == esc {
            seq, _ := t.r.Peek(2); t.r.Discard(2)
            switch string(seq) {
            case "[A": // up
                if sel > 0 { sel--; drawDropdown(sel) }
            case "[B": // down
                if sel < len(matches)-1 { sel++; drawDropdown(sel) }
            }
        } else if b == '\t' || b == '\r' {
            lastToken := prefix
            if i := strings.LastIndex(prefix, " "); i >= 0 {
                lastToken = prefix[i+1:]
            }
            delta := matches[sel][len(lastToken):]
            t.w.Write([]byte(delta))
            buf.WriteString(delta)
            *idx += len(delta)

            t.w.Write([]byte("\0337"))
            for range matches {
                t.w.Write([]byte("\r\n\033[K"))
            }
            t.w.Write([]byte("\0338"))

            return
        } else {
            t.r.UnreadByte()
            return
        }
    }
}