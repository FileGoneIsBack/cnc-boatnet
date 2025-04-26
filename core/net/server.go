package net

import (
	"api/core/database/site"
	"api/core/database/users"
	"api/core/models/log"
	"fmt"
	"io/ioutil"
	"net"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)


func Listener() {
	// Configure your desired ports here
	listenAddr := fmt.Sprintf("0.0.0.0:%s", site.Site.Telnet) // Telnet port
	sshListenAddr := fmt.Sprintf("0.0.0.0:%s", site.Site.SSH) // SSH port

	// Set up TCP listener for both SSH and Telnet
	tcpListener, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		log.Fatalf("[NET] Failed to listen on Telnet %s: %v", listenAddr, err)
	}
	defer tcpListener.Close()


	// SSH server configuration
	sshConfig := &ssh.Server{
		Addr:            ":" + site.Site.SSH,
		Handler:         sessionHandler,
		PasswordHandler: passwordHandler,
	}
	keyParser("assets/cert/key.cat", sshConfig) //fix

	// Start SSH server in a separate goroutine
	go func() {
		err := sshConfig.ListenAndServe()
		if err != nil {
			log.Fatal("[SSH] Server failed: ", err)
		}
	}()

	log.Printf("CNC Started! | Telnet: %s | SSH: %s", listenAddr, sshListenAddr)

	// Accept incoming connections for Telnet as well
	for {
		telnetConn, err := tcpListener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		log.Printf("New Telnet connection from: %s", telnetConn.RemoteAddr())
		go handler(telnetConn)
	}
}

func keyParser(file string, srv *ssh.Server) {
	pemBytes, err := ioutil.ReadFile(file)
	if err != nil {
		fmt.Println(err)
		return
	}
	hostKey, err := gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		fmt.Println(err)
		return
	}
	srv.AddHostKey(hostKey)
}

func sessionHandler(session ssh.Session) {
	NewAdmin(session).Handle()
}
func passwordHandler(ctx ssh.Context, pass string) bool {
	passwd := []byte(pass)
	user, err := users.Container.GetUser(ctx.User())
	if err != nil {
		log.Println(err)
		return false
	} else if !users.IsKey(user, passwd) {
		return false
	}
	return true
}
