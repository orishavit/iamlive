package iamlivecore

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime/pprof"
)

// CLI args
var providerFlag *string
var setiniFlag *bool
var profileFlag *string
var failsonlyFlag *bool
var outputFileFlag *string
var refreshRateFlag *int
var sortAlphabeticalFlag *bool
var hostFlag *string
var modeFlag *string
var bindAddrFlag *string
var caBundleFlag *string
var caKeyFlag *string
var accountIDFlag *string
var backgroundFlag *bool
var debugFlag *bool
var forceWildcardResourceFlag *bool
var cpuProfileFlag = flag.String("cpu-profile", "", "write a CPU profile to this file (for performance testing purposes)")

func parseConfig() {
	provider := "aws"
	setIni := false
	profile := "default"
	failsOnly := false
	outputFile := ""
	refreshRate := 0
	sortAlphabetical := false
	host := "127.0.0.1"
	mode := "csm"
	bindAddr := "0.0.0.0:10080"
	caBundle := "~/.iamlive/ca.pem"
	caKey := "~/.iamlive/ca.key"
	accountID := ""
	background := false
	debug := false
	forceWildcardResource := false

	providerFlag = flag.String("provider", provider, "the cloud service provider to intercept calls for")
	setiniFlag = flag.Bool("set-ini", setIni, "when set, the .aws/config file will be updated to use the CSM monitoring or CA bundle and removed when exiting")
	profileFlag = flag.String("profile", profile, "use the specified profile when combined with --set-ini")
	failsonlyFlag = flag.Bool("fails-only", failsOnly, "when set, only failed AWS calls will be added to the policy, csm mode only")
	outputFileFlag = flag.String("output-file", outputFile, "specify a file that will be written to on SIGHUP or exit")
	refreshRateFlag = flag.Int("refresh-rate", refreshRate, "instead of flushing to console every API call, do it this number of seconds")
	sortAlphabeticalFlag = flag.Bool("sort-alphabetical", sortAlphabetical, "sort actions alphabetically")
	hostFlag = flag.String("host", host, "host to listen on for CSM")
	modeFlag = flag.String("mode", mode, "the listening mode (csm,proxy)")
	bindAddrFlag = flag.String("bind-addr", bindAddr, "the bind address for proxy mode")
	caBundleFlag = flag.String("ca-bundle", caBundle, "the CA certificate bundle (PEM) to use for proxy mode")
	caKeyFlag = flag.String("ca-key", caKey, "the CA certificate key to use for proxy mode")
	accountIDFlag = flag.String("account-id", accountID, "the AWS account ID to use in policy outputs within proxy mode")
	backgroundFlag = flag.Bool("background", background, "when set, the process will return the current PID and run in the background without output")
	debugFlag = flag.Bool("debug", debug, "dumps associated HTTP requests when set in proxy mode")
	forceWildcardResourceFlag = flag.Bool("force-wildcard-resource", forceWildcardResource, "when set, the Resource will always be a wildcard")
}

func Run() {
	parseConfig()

	flag.Parse()

	if *providerFlag != "aws" {
		*modeFlag = "proxy"
	}

	if *backgroundFlag {
		args := os.Args[1:]
		for i := 0; i < len(args); i++ {
			if args[i] == "-background" || args[i] == "--background" {
				args = append(args[:i], args[i+1:]...)
				break
			}
		}
		cmd := exec.Command(os.Args[0], args...)
		cmd.Start()
		fmt.Println(cmd.Process.Pid)
		os.Exit(0)
	}

	if *cpuProfileFlag != "" {
		f, err := os.Create(*cpuProfileFlag)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	loadMaps()
	readServiceFiles()
	createProxy(*bindAddrFlag)
}
