// Remote tunnel (port forwarding) using x/crypto/ssh
//
// Eli Bendersky [https://eli.thegreenplace.net]
// This code is in the public domain.
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/lodmev/go/log"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func getHomePath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
	} else {
		return os.Getenv("HOME")
	}
}
func sshConfigPath(filename string) string {
	return filepath.Join(getHomePath(), ".ssh", filename)
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
func getSSHConnection(addr *string, config *ssh.ClientConfig) (client *ssh.Client) {
	var (
		sshConnErr error
	)
	client, sshConnErr = ssh.Dial("tcp", *addr, config)
	if sshConnErr != nil {
		log.Errf("can't connect to SSH '%s' : %v.", *addr, sshConnErr).Msg("Will try reconnect")
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			client, sshConnErr = ssh.Dial("tcp", *addr, config)
			if sshConnErr != nil {
				log.Printf("error while connecting to ssh: %v", sshConnErr)
			} else {
				return
			}
			log.Trace().Msg("reconnecting ssh...")
		}
	}
	return
}
func getListener(listFunc func(string, string) (net.Listener, error),
	listenAddr *string) (listener net.Listener, fatalErr error) {
	var err error
	listener, err = listFunc("tcp", *listenAddr)
	if err != nil {
		log.Trace().Msgf("can't listen '%s' will try again", *listenAddr)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			listener, err = listFunc("tcp", *listenAddr)
			if err != nil {
				if err.Error() == "EOF" {
					fatalErr = err
					return
				}
				switch err.(type) {
				case *net.OpError:
					fatalErr = err
					return
				default:
					log.Trace().Msgf("can't listen adress '%s': %v ", *listenAddr, err)
				}

			} else {
				return
			}
			log.Trace().Msg("trying to reopen listener")
		}

	}
	return
}
func makeTunnel(sshClient *ssh.Client, tun Tunnel, wg *sync.WaitGroup, sshErr chan error) {
	defer wg.Done()
	dialAdress := tun.addres[tun.tunnelType.opposite()]
	listenAdress := tun.addres[tun.tunnelType]
	var listenFunc func(string, string) (net.Listener, error)
	var dialFunc func(string, string) (net.Conn, error)
	var tunStr string
	switch tun.tunnelType {
	case Local:
		listenFunc = net.Listen
		dialFunc = sshClient.Dial
		tunStr = fmt.Sprintf("from 'local:%s' to 'remote:%s'", listenAdress, dialAdress)
	case Remote:
		listenFunc = sshClient.Listen
		dialFunc = net.Dial
		tunStr = fmt.Sprintf("from 'remote:%s' to 'local:%s'", listenAdress, dialAdress)
	}
	log.Debug().Msgf("Esteblishing tunnel %s", tunStr)
	defer log.Printf("Canceling tunnel %s", tunStr)
	listener, err := getListener(listenFunc, &listenAdress)
	// getListener returning only critical error
	if err != nil {
		log.Trace().Msgf("listen adress fail, tunnel %s will be closed", tunStr)
		return
	}
	defer listener.Close()
Loop:
	for {
		select {
		case <-sshErr:
			log.Trace().Msgf("ssh has problems closing tunnel %s", tunStr)
			break Loop
		default:
			listenConn, err := listener.Accept()
			if err != nil {
				log.Trace().Msgf("can't accept listen conn: %v", err)
				break Loop
			}
			dialConn, err := dialFunc("tcp", dialAdress)
			if err != nil {
				log.Trace().Msgf("unable to dial %v", err)
				listenConn.Close()
				continue
			}
			go handleConn(dialConn, listenConn)
		}
	}
	log.Trace().Msgf("exiting from tunnel %s", tunStr)

}

func handleConn(local, remote net.Conn) {
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

func main() {
	setup()
	log.Info().Msg("Starting service...")
	handlersCount := len(tunnels)
	var wg sync.WaitGroup
	for {
		sshConfig := createSshConfig(*username, *keyFile)
		sshClient := getSSHConnection(remoteServer, sshConfig)
		log.Info().Msgf("SSH connection to '%s' established", *remoteServer)
		sshConnError := make(chan error, handlersCount)
		defer sshClient.Close()
		go func(connError chan error) {
			err := sshClient.Conn.Wait()
			log.Error().Err(fmt.Errorf("SSH connection lost, will try reconnect")).Send()
			for _, tun := range tunnels {
				connError <- err
				if tun.tunnelType == Local {
					//dialing local for accept listener and unblock loop
					net.Dial("tcp", tun.addres[Local])
				}
			}
		}(sshConnError)
		for _, tun := range tunnels {
			wg.Add(1)
			go makeTunnel(sshClient, tun, &wg, sshConnError)
		}
		wg.Wait()
		sshClient.Close()
		close(sshConnError)
		log.Trace().Msg("all tunnels closed")
	}
}