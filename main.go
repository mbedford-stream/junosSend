package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/Juniper/go-netconf/netconf"
	"github.com/fatih/color"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type cmdDataStruct struct {
	Description   string   `json:"description"`
	ReferenceData string   `json:"refID"`
	DeviceIPs     []string `json:"deviceIPs"`
	CommandList   []string `json:"cmdList"`
}

type rpcConfigSet struct {
	XMLName             xml.Name `xml:"configuration-information"`
	Text                string   `xml:",chardata"`
	ConfigurationOutput string   `xml:"configuration-output"`
}

type rpcCommandResponse struct {
	XMLName xml.Name `xml:"rpc-reply"`
	Output  string   `xml:"output"`
}

func credentials() (string, string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Username: ")
	username, _ := reader.ReadString('\n')

	fmt.Print("Enter Password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println("\nPassword typed: " + string(bytePassword))
	}
	password := string(bytePassword)

	fmt.Println()

	return strings.TrimSpace(username), strings.TrimSpace(password)
}

func FileExists(fileName string) bool {
	if _, err := os.Stat(fileName); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func FileIsADirectory(file string) bool {
	if stat, err := os.Stat(file); err == nil && stat.IsDir() {
		// path is a directory
		return true
	}
	return false
}

// FileExistsAndIsADirectory - tests a file
func FileExistsAndIsADirectory(file string) bool {
	if FileExists(file) && FileIsADirectory(file) {
		return true
	}
	return false
}

func FileExistsAndIsNotADirectory(file string) bool {
	if FileExists(file) && !FileIsADirectory(file) {
		return true
	}
	return false
}

func FileReadReturnLines(fileName string) ([]string, error) {
	var errSlice []string
	if !FileExists(fileName) {
		return errSlice, errors.New("file does not exist")
	}

	file, err := os.Open(fileName)
	if err != nil {
		return errSlice, errors.New("could not open file")
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	scanner.Split(bufio.ScanLines)

	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, nil
}

func ForceSelect(optQuestion, optOne, optTwo string) string {
	var goodSel bool
	var inputSelTrim string

	for validSel := false; !validSel; validSel = goodSel {
		inReader := bufio.NewReader(os.Stdin)
		fmt.Print(optQuestion)
		inputSel, _ := inReader.ReadString('\n')
		inputSelTrim = strings.Trim(inputSel, "\n")
		inputSelTrim = strings.Trim(inputSelTrim, "\n\r")
		inputSelTrim = strings.Trim(inputSelTrim, "\r")
		if strings.ToLower(inputSelTrim) == optOne || strings.ToLower(inputSelTrim) == optTwo {
			goodSel = true
		}
	}
	return strings.ToLower(inputSelTrim)
}

func getRPC(devIP string, devUser string, devPass string, rpcCommand string) *netconf.RPCReply {
	sshConfig := &ssh.ClientConfig{
		User:            devUser,
		Auth:            []ssh.AuthMethod{ssh.Password(devPass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	fmt.Printf("Connecting: %s\n", devIP)
	s, err := netconf.DialSSH(devIP, sshConfig)
	if err != nil {
		log.Fatalf("Error connecting: %s", err)
	}

	defer s.Close()

	res, err2 := s.Exec(netconf.RawMethod(rpcCommand))
	if err2 != nil {
		panic(err2)
	}
	return res
}

func checkIP(testIP string) bool {
	ipConv := net.ParseIP(testIP)
	// fmt.Println(ipConv)
	return ipConv != nil
}

func StringSliceContains(checkSlice []string, checkValue string) bool {
	for _, v := range checkSlice {
		if v == checkValue {
			return true
		}
	}

	return false
}

func commandOutputStripper(input string) string {
	t := strings.Replace(strings.Replace(input, "<output>", "", -1), "</output>", "", -1)
	return t
}

func main() {
	nameStr := "Junos Send"
	verStr := "0.0.5"

	fmt.Println("Hello there, I'm here to help you load Juniper configs.")

	var operationSelect string
	var commandsFile string
	var showDiffs bool
	var saveOuputs bool
	var printVer bool

	flag.StringVar(&operationSelect, "m", "s", "Choose the mode the script will operate in; Config(c), Operational(o), Select interactively(s)")
	flag.StringVar(&commandsFile, "f", "./example.json", "Location of input file")
	flag.BoolVar(&showDiffs, "d", true, "Show the resulting diff from any config commands")
	flag.BoolVar(&saveOuputs, "s", false, "Save the replies from Op commands to text files")
	flag.BoolVar(&printVer, "v", false, "Print script version")

	flag.Parse()

	if printVer {
		color.Green("%s\nVer: %s", nameStr, verStr)
		os.Exit(0)
	}

	operationSelect = strings.ToLower(operationSelect)
	commandsFile = strings.ToLower(commandsFile)

	if operationSelect != "c" && operationSelect != "o" && operationSelect != "s" {
		color.Red("Valid choices for '-m' are c for Config, o for Operational commands, or s for Select at runtime")
		os.Exit(0)
	}
	var cmdsFile string

	if commandsFile == "./example.json" {
		getFilePath := bufio.NewReader(os.Stdin)
		fmt.Printf("Please provide the file path for input information: ")
		cmdsFileRaw, _ := getFilePath.ReadString('\n')
		cmdsFile = strings.TrimSpace(cmdsFileRaw)

		if !FileExistsAndIsNotADirectory(cmdsFile) {
			fmt.Println("It appears that file is not where you think it is")
			os.Exit(0)
		}
	}

	fmt.Printf("Congratulations, you gave me %s and it exists!\n\n", cmdsFile)

	file, _ := ioutil.ReadFile(cmdsFile)
	var inputData cmdDataStruct
	jsonErr := json.Unmarshal([]byte(file), &inputData)
	if jsonErr != nil {
		color.Red("There was a problem reading the input file, is it correct JSON?")
		os.Exit(0)
	}

	var badIPs []string
	for _, i := range inputData.DeviceIPs {
		if !checkIP(i) {
			badIPs = append(badIPs, i)
		}
	}

	if len(badIPs) != 0 {
		color.Red("There was a problem with your inputs, please check:")
		for _, i := range badIPs {
			fmt.Printf("\t%s\n", i)
		}
		os.Exit(0)
	}
	var scriptMode string
	if operationSelect == "s" {
		scriptMode = ForceSelect("Please choose from config mode (c) or operational mode (o) (c/o): ", "c", "o")
	} else {
		scriptMode = operationSelect
	}

	var badCmds []string
	validConfigPrefix := []string{"set", "delete", "activate", "deactivate"}
	validOpPOrefix := []string{"show"}

	for _, i := range inputData.CommandList {
		if scriptMode == "c" {
			// fmt.Println(StringSliceContains(validConfigPrefix, strings.Split(i, " ")[0]))
			if !StringSliceContains(validConfigPrefix, strings.Split(i, " ")[0]) {
				badCmds = append(badCmds, i)
			}
		} else if scriptMode == "o" {
			if !StringSliceContains(validOpPOrefix, strings.Split(i, " ")[0]) {
				badCmds = append(badCmds, i)
			}
		} else {
			fmt.Println("What are we doing here?")
		}
	}

	if len(badCmds) != 0 {
		color.Red("Please check the following commands for allowed syntax: \n==========================================================\n")
		for _, i := range badCmds {
			fmt.Println(i)
		}
		os.Exit(0)
	}

	if scriptMode == "c" {
		fmt.Println("WE'RE CONFIGURING")
	}

	color.Green("Description:\n=====================\n%s\n\n", inputData.Description)
	color.Green("Reference:\n=====================\n%s\n\n", inputData.ReferenceData)
	color.Green("Devices:\n=====================\n")
	for _, i := range inputData.DeviceIPs {
		fmt.Printf("\t%s\n", i)
	}
	color.Green("Commands:\n=====================\n")
	for _, i := range inputData.CommandList {
		fmt.Printf("\t%s\n", i)
	}
	fmt.Printf("\n\n")

	continueAction := ForceSelect("Continue with sending of commands? (y/n)", "y", "n")

	if continueAction == "n" {
		color.Yellow("Quitting program, no commands or config items were sent.")
		os.Exit(0)
	}

	devUser, devPass := credentials()

	if scriptMode == "c" {
		fmt.Println("Proceding to load configs")
		for _, d := range inputData.DeviceIPs {
			fmt.Printf("\n\nConnecting to: %s\n===========================\n", d)
			var setString string
			for _, i := range inputData.CommandList {
				setString = fmt.Sprintf("%s\n%s", setString, i)
			}

			sshConfig := &ssh.ClientConfig{
				User:            devUser,
				Auth:            []ssh.AuthMethod{ssh.Password(devPass)},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			}

			s, err := netconf.DialSSH(fmt.Sprintf("%s:%s", d, "830"), sshConfig)
			if err != nil {
				log.Fatalf("Error connecting: %s", err)
			}

			defer s.Close()

			configLocked := true
			res, err := s.Exec(netconf.RawMethod("<lock-configuration/>"))
			if err != nil {
				color.Red("There was a problem locking the configuration, is someone/thing already editing?")
				color.Red("continuing without locking config")
				configLocked = false
				// panic(err)
			}
			rpcResp := res

			rpcCommand := fmt.Sprintf("<load-configuration action='set' format='text'><configuration-set>%s</configuration-set></load-configuration>", setString)
			res, err = s.Exec(netconf.RawMethod(rpcCommand))
			if err != nil {
				panic(err)
			}
			rpcResp = res

			rpcCommand = "<validate><source><candidate/></source></validate>"
			res, err = s.Exec(netconf.RawMethod(rpcCommand))
			if err != nil {
				color.Red("There was a problem validating the candidate config and commiting the change may fail.")
				// panic(err)
			}
			rpcResp = res

			color.Yellow("Config Diff:\n==========================================\n")
			rpcCommand = "<get-configuration compare='rollback' rollback='0' format='text'/>"
			res, err = s.Exec(netconf.RawMethod(rpcCommand))
			if err != nil {
				color.Red("Config changes could not be compared, please check the device manually.")
				// panic(err)
			}
			rpcResp = res

			var rpcReturned rpcConfigSet
			err = xml.Unmarshal([]byte(rpcResp.Data), &rpcReturned)
			if err != nil {
				log.Fatal(err)
			}
			color.Yellow(rpcReturned.ConfigurationOutput)

			confirmCommit := ForceSelect("\nCommit changes to device? (y/n):", "y", "n")
			if confirmCommit == "n" {
				rpcCommand = "<discard-changes/>"
				res, err = s.Exec(netconf.RawMethod(rpcCommand))
				if err != nil {
					color.Red("Config changes could not be rolled back, please check the device and rollback manually.")
					// panic(err)
				}
				rpcResp = res
				color.Green("\nConfig changes have been reverted")
			} else if confirmCommit == "y" {
				rpcCommand = fmt.Sprintf("<commit><comment>%s</comment></commit>", inputData.ReferenceData)
				res, err = s.Exec(netconf.RawMethod(rpcCommand))
				if err != nil {
					color.Red("Config changes could not be committed, please check the device and rollback manually.")
					// panic(err)
				}
				rpcResp = res
				color.Green("\nConfig changes commited")
			}

			if configLocked {
				rpcCommand = fmt.Sprintf("<unlock-configuration/>")
				res, err = s.Exec(netconf.RawMethod(rpcCommand))
				if err != nil {
					color.Red("Config could not be unlocked, is there an existing session?")
					// panic(err)
				}
				rpcResp = res
			}

		}
	} else if scriptMode == "o" {
		fmt.Println("Proceding to send operational commands")
		if saveOuputs && !FileExistsAndIsADirectory(fmt.Sprintf("%s", inputData.ReferenceData)) {
			err := os.Mkdir(fmt.Sprintf("%s", inputData.ReferenceData), 0755)
			if err != nil {
				log.Fatal(err)
			}
		}

		for _, d := range inputData.DeviceIPs {
			if saveOuputs && !FileExistsAndIsNotADirectory(fmt.Sprintf("%s", inputData.ReferenceData)) {
				outputFile := fmt.Sprintf("%s/%s.txt", inputData.ReferenceData, strings.Replace(d, ".", "_", -1))
				_, err := os.OpenFile(outputFile, os.O_CREATE, 0755)
				if err != nil {
					color.Red("Problems opening output file: %s", outputFile)
					continue
				}
			}
			fmt.Printf("\n\nConnecting to: %s\n===========================\n", d)
			var setString string
			for _, i := range inputData.CommandList {
				setString = fmt.Sprintf("%s\n%s", setString, i)
			}

			sshConfig := &ssh.ClientConfig{
				User:            devUser,
				Auth:            []ssh.AuthMethod{ssh.Password(devPass)},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			}

			s, err := netconf.DialSSH(fmt.Sprintf("%s:%s", d, "830"), sshConfig)
			if err != nil {
				log.Fatalf("Error connecting: %s", err)
			}

			defer s.Close()

			for _, c := range inputData.CommandList {
				commandContent := fmt.Sprintf("<command format=\"ascii\">%s</command>", c)

				res, err := s.Exec(netconf.RawMethod(commandContent))
				if err != nil {
					color.Red("There was a problem executing the command, please check your syntax:\n\t%s", c)
					continue
				}
				cmdOutput := commandOutputStripper(res.Data)

				if saveOuputs {
					outputFile := fmt.Sprintf("%s/%s.txt", inputData.ReferenceData, strings.Replace(d, ".", "_", -1))
					f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY, 0755)
					if err != nil {
						color.Red("Problems opening output file: %s", outputFile)
						continue
					}
					defer f.Close()
					_, err = f.WriteString(c + "\n==============================================" + cmdOutput)
					if err != nil {
						color.Red("problems writing file")
						fmt.Println(err)
					}

				}

				color.Green(cmdOutput)
			}
			if saveOuputs {
				fmt.Printf("Outputs written to: %s/%s\n", inputData.ReferenceData, strings.Replace(d, ".", "_", -1))
			}

		}
	}
}
