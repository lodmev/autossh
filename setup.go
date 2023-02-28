package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gobwas/flagutil"
	"github.com/gobwas/flagutil/parse/args"
	"github.com/gobwas/flagutil/parse/file"
	"github.com/gobwas/flagutil/parse/file/toml"
	"github.com/lodmev/go/log"
)
var (
	tunnels Tunnels
	remoteServer, username, keyFile *string
)
type TunnelType uint
func (t TunnelType) String() string {
	return [...]string{"local", "remote"}[t]
}
func (t TunnelType) opposite() TunnelType {
	if t == Local {
		return Remote
	}
	return Local
}
const (
	Local TunnelType = iota
	Remote 
)

type Tunnel struct {
	addres map[TunnelType] string
	tunnelType TunnelType
}
type Tunnels []Tunnel
func (t *Tunnels) Set(v string) error{
	tun := Tunnel{}
	tun.addres = make(map[TunnelType]string, 2)
	//get first symbol, that must be "-R" or "-L"
	tType, adr ,found := strings.Cut(v, " ")
	if !found {return fmt.Errorf("unable find tunnel type in string '%q'", v)}
	switch tType {
	case "-R", "R":
		tun.tunnelType = Remote
	case "-L", "L":
		tun.tunnelType = Local
	default:
		return fmt.Errorf("unknown tunnel type: %q", tType)
	}
	adresses := strings.Split(adr, ":")
	switch len(adresses) {
	case 3:
		tun.addres[tun.tunnelType] = "127.0.0.1" + ":" + adresses[0]
		tun.addres[tun.tunnelType.opposite()] = adresses[1] + ":" + adresses[2]
	case 4:
	tun.addres[tun.tunnelType] = adresses[0] + ":" + adresses[1]
		tun.addres[tun.tunnelType.opposite()] = adresses[2] + ":" + adresses[3]
	default:
		return fmt.Errorf("unable parse adresses from string: %q", tType)
	}
	
	*t = append(*t, tun)
	return nil
}
func (t *Tunnels) String() string {
	return fmt.Sprint(*t)
}
func setup() {
	fs := flag.NewFlagSet("autossh", flag.ExitOnError )
	var logConf *log.Config
	// This flag will be required by the file.Parser below.
	_ = fs.String(
		"config", "config.toml", 
		"path to configuration file",
	)
	username = fs.String("user", "", "username for ssh")
	keyFile = fs.String("keyfile", "id_rsa", "file with private key for SSH authentication")
	remoteServer = fs.String( "remote_server", "", "adress of remote server")
	fs.Var(&tunnels,"tunnels","tunnel params like \"-L 8080:127.0.0.1:8080\"")
	flagutil.Subset(fs, "logger", func(sub *flag.FlagSet) {
	logConf = log.ExportConf(sub)
	})

	flagutil.Parse(context.Background(), fs,
	flagutil.WithParser(&args.Parser{
		Args: os.Args[1:],
	}),
	flagutil.WithParser(&file.Parser{
		Lookup: file.LookupFlag(fs, "config"),
		Syntax: new(toml.Syntax),
	},
	flagutil.WithStashName("config"),
),
)
	if err:= logConf.Setup(); err != nil {
		log.Errf("fault setup configs of logger: %w", err)
	}

}