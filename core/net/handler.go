package net

import (
	"api/core/database/users"
	"api/core/models"
	"api/core/models/log"
	"api/core/net/commands"
	"api/core/net/sessions"
	"api/core/net/term"
	"fmt"
	"net"
	"strings"
	"time"
	"sort"
)

var SpinnerChars = []string{"|", "/", "-", "\\"}

func handler(conn net.Conn) {
	defer conn.Close()
	conn.Write([]byte(fmt.Sprintf("\033]0;%s - Login\007", models.Config.Name)))
	//more
	buf := make([]byte, 64)
	if _, err := conn.Read(buf); err != nil {
		log.Println("Failed to read initial data: ", err)
		return
	}
	allCommands := make([]string, 0, len(commands.Commands))
	for name := range commands.Commands {
		allCommands = append(allCommands, name)
	}
	sort.Strings(allCommands)
	attackMethods := commands.AvailableAttackMethods()
	completer := func(input string) []string {
		log.Printf("[COMPLETER] input=%q", input)
	
		parts := strings.Split(input, " ")
		if len(parts) == 0 {
			log.Printf("[COMPLETER] no parts, returning nil")
			return nil
		}
	
		// completing the command name
		if len(parts) == 1 {
			suggestions := commands.Filter(parts[0], allCommands)
			log.Printf("[COMPLETER] command suggestions for %q → %v", parts[0], suggestions)
			return suggestions
		}
	
		// only handle attack’s 5th token
		if parts[0] == "attack" {
			if len(parts) >= 5 {
				prefix := parts[4]
				suggestions := commands.Filter(prefix, attackMethods)
				log.Printf("[COMPLETER] attack-methods for prefix=%q → %v", prefix, suggestions)
				return suggestions
			}
			log.Printf("[COMPLETER] attack but not enough tokens (got %d), skipping", len(parts))
			return nil
		}
	
		log.Printf("[COMPLETER] first token %q isn’t attack, skipping", parts[0])
		return nil
	}
	
	tm := term.New(conn, completer)
	defer tm.Close()
	

	username, err := tm.ReadLine("Username# ")
	if err != nil {
		return
	}
	pass, err := tm.ReadLine("Password# ")
	if err != nil {
		return
	}

	user, err := users.Container.GetUser(username)
	if err != nil {
		fmt.Fprintf(conn, "Database error: %v\r\n", err)
		time.Sleep(5 * time.Second)
		conn.Close()
		return
	}


	if !users.IsKey(user, []byte(pass)) {
		fmt.Fprintf(conn, "Invalid password...\r\n")
		time.Sleep(5 * time.Second)
		conn.Close()
		return
	}

	var Session = &sessions.Session{
		User: user,
		Conn: conn,
	}

	Session.CreateCookie()

	for _, session := range sessions.Sessions {
		if session.User.Username == username {
			fmt.Fprintf(conn, "Session Already Open!")
			log.Println(session.User.Username + " already has a session open!")
			return
		}
	}

	sessions.SessionMutex.Lock()
	sessions.Sessions[Session.ID] = Session
	sessions.SessionMutex.Unlock()
	var role string
	if users.HasPermission(Session.User, "admin") {
		role = "admin"
	} else {
		role = "user"
	}
	//title
	go func() {
		i := 0

		for {
			time.Sleep(time.Second)

			message := fmt.Sprintf("\033]0; [%s] %s CnC - Username [%s] - Online [%d] - Rank [%s] \007",
				SpinnerChars[i%len(SpinnerChars)], models.Config.Name, Session.User.Username, sessions.Count(), role)

			if _, err := conn.Write([]byte(message)); err != nil {
				if sessions.RemoveSession(Session.ID) {
					log.Println(Session.User.Username + " Session Closed!")
				}
				conn.Close()
				break
			}

			i++
		}
	}()
	commands.Commands["splash-home"].Exec(Session, nil)
	// handler
	for {
		cmd, err := tm.ReadLine(fmt.Sprintf("%s@%s# ", Session.User.Username, models.Config.Name))
		if err != nil {
			return
		}
		cmdlist := strings.Split(cmd, " ")
		if !commands.IsCommand(cmdlist[0]) {
			fmt.Fprintf(conn, "Command (%s) is Invalid\r\n", cmdlist[0])
		} else {
			commands.Commands[cmdlist[0]].Exec(Session, cmdlist)
		}
	}
}
