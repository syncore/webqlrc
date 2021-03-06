// webqlrc.go - The entry point. Parse flags and start the application.
package main

import (
	"flag"
	"fmt"
	"os"
	"webqlrc/bridge"
	"webqlrc/config"
	"webqlrc/rcon"
	"webqlrc/web"
)

const (
	bothConfigureFlag = "config"
	rconConfigureFlag = "rconconfig"
	webConfigureFlag  = "webconfig"
)

var (
	doRconAndWebConfig bool
	doRconConfig       bool
	doWebConfig        bool
)

func init() {

	flag.BoolVar(&doRconAndWebConfig, bothConfigureFlag, false,
		"Generate both web and RCON configuration files")

	flag.BoolVar(&doRconConfig, rconConfigureFlag, false,
		"Generate the RCON configuration file")

	flag.BoolVar(&doWebConfig, webConfigureFlag, false,
		"Generate the web configuration file")
}

func main() {
	flag.Parse()

	// --config and (--rconconfig or --webconfig) are mutually exclusive
	if doRconAndWebConfig && (doRconConfig || doWebConfig) {
		fmt.Printf("You cannot specify flag --%s with either --%s or --%s\n",
			bothConfigureFlag, rconConfigureFlag, webConfigureFlag)
		os.Exit(1)
	}
	// Parse flags, if any
	// --config
	if doRconAndWebConfig {
		fmt.Printf("webqlrc %s: Create web and RCON configuration files\n",
			config.Version)
		err := config.CreateRconConfig()
		if err != nil {
			fmt.Printf("Unable to create RCON configuration: %s\n", err)
		}
		err = config.CreateWebConfig()
		if err != nil {
			fmt.Printf("Unable to create web configuration: %s\n", err)
		}
		os.Exit(0)
	}
	// --rconconfig
	if doRconConfig {
		fmt.Printf("webqlrc %s: Create RCON configuration file\n",
			config.Version)
		err := config.CreateRconConfig()
		if err != nil {
			fmt.Printf("Unable to create RCON configuration: %s\n", err)
		}
	}
	// --webconfig
	if doWebConfig {
		fmt.Printf("webqlrc %s: Create web configuration file\n",
			config.Version)
		err := config.CreateWebConfig()
		if err != nil {
			fmt.Printf("Unable to create web configuration: %s\n", err)
		}
	}
	if doRconAndWebConfig || doRconConfig || doWebConfig {
		os.Exit(0)
	}

	// Verify existence and ability to read config files
	_, err := config.ReadConfig(config.RCON)
	if err != nil {
		fmt.Printf("Could not read RCON configuration file '%s' in '%s' directory\n",
			config.RconConfigurationFilename, config.ConfigurationDirectory)
		fmt.Printf("You must first generate the file with: %s --%s or --%s\n",
			os.Args[0], bothConfigureFlag, rconConfigureFlag)
		os.Exit(1)
	}
	_, err = config.ReadConfig(config.WEB)
	if err != nil {
		fmt.Printf("Could not read web configuration file: '%s' in '%s' directory\n",
			config.WebConfigurationFilename, config.ConfigurationDirectory)
		fmt.Printf("You must first generate the file with: %s --%s or --%s\n",
			os.Args[0], bothConfigureFlag, webConfigureFlag)
		os.Exit(1)
	}
	// Verify existence and ability to read web user file
	err = config.VerifyWebUserFile()
	if err != nil {
		fmt.Printf("Error reading web user file: '%s' in '%s' directory\n",
			config.WebUserFilename, config.ConfigurationDirectory)
		fmt.Printf("To generate a new web user file, run the web configuration with: %s --%s or --%s\n",
			os.Args[0], bothConfigureFlag, webConfigureFlag)
		os.Exit(1)
	}

	// Everything looks good
	go bridge.MessageBridge.PassMessages()
	fmt.Printf("Starting webqlrc v%s\n", config.Version)
	rcon.Start()
	web.Start()
}
