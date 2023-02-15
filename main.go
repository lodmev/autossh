// Remote tunnel (port forwarding) using x/crypto/ssh
//
// Eli Bendersky [https://eli.thegreenplace.net]
// This code is in the public domain.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func sshConfigPath(filename string) string {
	return filepath.Join(os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"), ".ssh", filename)
}

func createSshConfig(username, keyFile string) *ssh.ClientConfig {
	knownHostsCallback, err := knownhosts.New(sshConfigPath("known_hosts"))
	if err != nil {
		log.Fatal(err)
	}

	key, err := os.ReadFile(sshConfigPath(keyFile))
	if err != nil {
		log.Fatalf("unable to read private key: %v", err)
	}

	// Create the Signer for this private key.
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Fatalf("unable to parse private key: %v", err)
	}

	// An SSH client is represented with a ClientConn.
	//
	// To authenticate with the remote server you must pass at least one
	// implementation of AuthMethod via the Auth field in ClientConfig,
	// and provide a HostKeyCallback.
	return &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback:   knownHostsCallback,
		HostKeyAlgorithms: []string{ssh.KeyAlgoECDSA256, ssh.KeyAlgoED25519},
	}
}

func main() {
	addr := "lodmev.duckdns.org:8022"
	username := flag.String("user", "", "username for ssh")
	keyFile := flag.String("keyfile", "", "file with private key for SSH authentication")
	remotePort := flag.String("rport", "", "remote port for tunnel")
	localPort := flag.String("lport", "", "local port for tunnel")
	flag.Parse()

	config := createSshConfig(*username, *keyFile)

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		log.Fatal("Failed to dial: ", err)
	}
	defer client.Close()

	listener, err := client.Listen("tcp", "127.0.0.1:"+*remotePort)
	if err != nil {
		log.Fatalf("unable to listen on remove: %v", err)
	}
	defer listener.Close()
	done := make(chan struct{}, 1)

	go func() {
		for {
			local, err := net.Dial("tcp", "localhost:"+*localPort)
			if err != nil {
				log.Fatalf("unable to dial local: %v", err)
			}
			remote, err := listener.Accept()
			if err != nil {
				log.Fatal(err)
			}

			fmt.Println("tunnel established with", local.LocalAddr())
			runTunnel(local, remote)
			fmt.Println("tunnel closed with", local.LocalAddr())
		}
	}()
	<-done
}

func runTunnel(local, remote net.Conn) {
	defer remote.Close()
	defer local.Close()
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(local, remote)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(remote, local)
		done <- struct{}{}
	}()

	<-done
}