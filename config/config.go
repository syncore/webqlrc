// config.go - Generate rcon and/or web configuration files
package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var newline string = getNewLineForOS()

const (
	defaultRconShowOnConsole             = false
	defaultRconPollTimeOut               = 50
	defaultWebMaxMessageSize             = 512
	defaultWebPongTimeout                = 60
	defaultWebSendTimeout                = 10
	ConfigurationDirectory               = "conf"
	RconConfigurationFilename            = "rcon.conf"
	WebConfigurationFilename             = "web.conf"
	Version                              = "0.1"
	RCON                      configType = 0
	WEB                       configType = 1
)

type configType int

type rconConfig struct {
	QlZmqHost            string
	QlZmqRconPort        int
	QlZmqRconPassword    string
	QlZmqRconPollTimeout time.Duration
	QlZmqShowOnConsole   bool
}

type webConfig struct {
	WebMaxMessageSize int64
	WebPongTimeout    int
	WebSendTimeout    int
	WebServerPort     int
	WebAdminUser      string
	WebAdminPassword  string
}

type Config struct {
	Rcon *rconConfig
	Web  *webConfig
}

func getNewLineForOS() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	} else {
		return "\n"
	}
}

func ReadConfig(ct configType) (*Config, error) {
	var fpath string
	cfg := &Config{}

	if ct == RCON {
		fpath = path.Join(ConfigurationDirectory, RconConfigurationFilename)
		cfg.Rcon = &rconConfig{}
	} else if ct == WEB {
		fpath = path.Join(ConfigurationDirectory, WebConfigurationFilename)
		cfg.Web = &webConfig{}
	}

	f, err := os.Open(fpath)
	defer f.Close()
	if err != nil {
		return nil, fmt.Errorf("Unable to read config file.")
	}

	r := bufio.NewReader(f)
	dec := json.NewDecoder(r)

	if ct == RCON {
		err = dec.Decode(cfg.Rcon)
	} else if ct == WEB {
		err = dec.Decode(cfg.Web)
	}

	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func CreateRconConfig() error {
	reader := bufio.NewReader(os.Stdin)
	rconcfg := &rconConfig{
		QlZmqRconPollTimeout: defaultRconPollTimeOut,
		QlZmqShowOnConsole:   defaultRconShowOnConsole,
	}

	validHost := false
	for !validHost {
		fmt.Print("Enter your ZeroMQ QL RCON hostname or IP address: ")

		hostname, err := getRconHostname(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			rconcfg.QlZmqHost = hostname
			validHost = true
		}
	}
	validPort := false
	for !validPort {
		fmt.Print("Enter your ZeroMQ QL RCON port number: ")

		port, err := getPort(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			rconcfg.QlZmqRconPort = port
			validPort = true
		}
	}
	validPassword := false
	for !validPassword {

		fmt.Print("Enter your ZeroMQ QL RCON password: ")
		password, err := getPassword(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			rconcfg.QlZmqRconPassword = password
			validPassword = true
		}
	}
	err := writeConfigFile(rconcfg)
	if err != nil {
		return fmt.Errorf("Unable to create RCON configuration file: %s", err)
	}
	fmt.Printf("Created RCON configuration file '%s' in '%s' directory.\n",
		RconConfigurationFilename, ConfigurationDirectory)
	return nil
}

func CreateWebConfig() error {
	reader := bufio.NewReader(os.Stdin)
	webcfg := &webConfig{
		WebMaxMessageSize: defaultWebMaxMessageSize,
		WebPongTimeout:    defaultWebPongTimeout,
		WebSendTimeout:    defaultWebSendTimeout,
	}
	validPort := false
	for !validPort {
		fmt.Print("Enter the port to use for the web interface: ")
		port, err := getPort(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			webcfg.WebServerPort = port
			validPort = true
		}
	}
	validUser := false
	for !validUser {

		fmt.Print("Enter the admin user name to use for the web interface: ")
		user, err := getWebUser(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			webcfg.WebAdminUser = user
			validUser = true
		}
	}
	validPassword := false
	for !validPassword {

		fmt.Print("Enter the admin password to use for the web interface: ")
		password, err := getPassword(reader)
		if err != nil {
			fmt.Println(err)
		} else {
			pw, err := generateBcryptPassword(password)
			if err != nil {
				fmt.Println(err)
			} else {
				webcfg.WebAdminPassword = string(pw)
				validPassword = true
			}
		}
	}

	err := writeConfigFile(webcfg)
	if err != nil {
		return fmt.Errorf("Unable to create web configuration file: %s", err)
	}
	fmt.Printf("Created web configuration file '%s' in '%s' directory.\n",
		WebConfigurationFilename, ConfigurationDirectory)

	return nil
}

func createConfigDirectory() error {
	err := os.Mkdir(ConfigurationDirectory, 0777)
	if err != nil {
		return err
	}

	return nil

}

func writeConfigFile(cfgfiletype interface{}) error {
	err := createConfigDirectory()
	if err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("Unable to create '%s' directory: %s",
				ConfigurationDirectory, err)
		}
	}
	var cfgb []byte
	var cmsg string

	switch cfgfiletype := cfgfiletype.(type) {
	default:
		return fmt.Errorf("Unexpected config file type %T\n", cfgfiletype)
	case *rconConfig:
		cmsg = "RCON"
		cfgb, err = json.Marshal(cfgfiletype)
		if err != nil {
			return fmt.Errorf("Error encoding %s configuration: %s", err, cmsg)
		}
	case *webConfig:
		cmsg = "web"
		cfgb, err = json.Marshal(cfgfiletype)
		if err != nil {
			return fmt.Errorf("Error encoding %s configuration: %s", err, cmsg)
		}
	}

	err = os.Chdir(ConfigurationDirectory)
	if err != nil {
		return fmt.Errorf("Unable to change to configuration directory '%s': %s",
			ConfigurationDirectory, err)
	}

	var cfgfile *os.File
	var fn string
	if cmsg == "RCON" {
		fn = RconConfigurationFilename
	} else if cmsg == "web" {
		fn = WebConfigurationFilename
	}

	cfgfile, err = os.Create(fn)
	defer cfgfile.Close()
	if err != nil {
		return fmt.Errorf("Unable to create %s configuration file '%s': %s",
			cmsg, fn, err)
	}
	cfgfile.Sync()

	writer := bufio.NewWriter(cfgfile)
	_, err = writer.Write(cfgb)
	writer.Flush()
	if err != nil {
		return fmt.Errorf("Unable to write %s configuration file '%s' to '%s' directory: %s",
			cmsg, fn, ConfigurationDirectory, err)
	}
	err = os.Chdir("..")
	if err != nil {
		return fmt.Errorf("Unable to change directory: %s", err)
	}
	return nil
}

func getRconHostname(r *bufio.Reader) (string, error) {
	hostname, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("Unable to read hostname: %s", err)
	}
	if hostname == newline {
		return "", errors.New("Hostname was not specified.")
	}
	return strings.Trim(hostname, newline), nil
}

func getPassword(r *bufio.Reader) (string, error) {
	password, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("Unable to read password: %s", err)
	}
	if password == newline {
		return "", errors.New("Password was not specified.")
	}
	return strings.Trim(password, newline), nil

}

func generateBcryptPassword(password string) ([]byte, error) {
	pw := []byte(strings.Trim(password, newline))
	encrypted, err := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("Unable to generte encrypted password: %s", err)
	}

	return encrypted, nil
}

func getPort(r *bufio.Reader) (int, error) {
	pstr, err := r.ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("Unable to read port: %s", err)
	}
	if pstr == newline {
		return 0, errors.New("Port was not specified.")
	}
	port, err := strconv.Atoi(strings.Trim(pstr, newline))
	if err != nil {
		return 0, errors.New("Invalid port. Port must be a number from 1-65535")
	}
	if port < 1 || port > 65535 {
		return 0, errors.New("Invalid port. Port must be a number from 1-65535")
	}

	return port, nil
}

func getWebUser(r *bufio.Reader) (string, error) {
	user, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("Unable to read user name: %s", err)
	}
	if user == newline {
		return "", errors.New("User name was not specified.")
	}

	return strings.Trim(user, newline), nil
}

// test
func passwordsMatch(hashed, password []byte) bool {
	result := bcrypt.CompareHashAndPassword(hashed, password)
	if result != nil {
		fmt.Println("ERROR: Hashed password does not match password!")
		return false
	} else {
		fmt.Println("SUCCESS: Passwords match!!!!!")
		return true
	}
}
