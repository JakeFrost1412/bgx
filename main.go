package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

// Color codes for terminal output (can be disabled)
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
)

var useColor = true

func init() {
	// Disable color if NO_COLOR env var is set or not a terminal
	if os.Getenv("NO_COLOR") != "" {
		useColor = false
	}
}

func colorize(color, text string) string {
	if !useColor {
		return text
	}
	return color + text + colorReset
}

// CmdResult holds the output and error from a command
type CmdResult struct {
	Output string
	Err    error
}

func showHelp() {
	exe := path.Base(os.Args[0])
	fmt.Printf(`%s - Background systemd-run helper

Usage:
  %s [options] <command...>    Start command as transient systemd user service
  %s -l | --list               List all cmd-* units grouped by state
  %s -s <unit> | --status      Show status and logs for a specific unit
  %s --clean                   Clean dead/failed/inactive cmd-* units
  %s -k <unit> | --kill        Stop specific running unit
  %s -K | --kill-all           Stop all running cmd-* units

Options:
  -v, --verbose                Show detailed information (descriptions)
  -f, --follow                 Follow logs in real-time (use with -s)
  -y, --yes                    Answer yes to all confirmations
  --no-color                   Disable colored output
  -h, --help                   Show this help message

Examples:
  %s gowitness report server
  %s -l
  %s -l -v
  %s -s cmd-1732459032
  %s -s cmd-1732459032 -f
  %s -k cmd-1732459032
  %s -K
  %s --clean

Environment:
  NO_COLOR                     Disable colored output if set

`, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe, exe)
}

func runCmdCapture(name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func runCmdPassthru(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func checkSystemdAvailable() error {
	// Try pidof systemd first
	if _, err := exec.LookPath("pidof"); err == nil {
		if out, err := runCmdCapture("pidof", "systemd"); err == nil {
			if strings.TrimSpace(out) != "" {
				return nil
			}
		}
	}
	// Fallback: read /proc/1/comm (works on Linux)
	if data, err := os.ReadFile("/proc/1/comm"); err == nil {
		if strings.TrimSpace(string(data)) == "systemd" {
			return nil
		}
	}
	return fmt.Errorf("systemd is not available on this system (init process is not systemd)")
}

func mustHaveCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command '%s' is not available in PATH", name)
	}
	return nil
}

// UnitInfo represents information about a systemd unit
type UnitInfo struct {
	Name        string
	State       string
	Description string
}

// parseUnitLine extracts unit information from systemctl list output
func parseUnitLine(line string) *UnitInfo {
	// systemctl output format: UNIT LOAD ACTIVE SUB DESCRIPTION
	// We want UNIT, ACTIVE (state), and DESCRIPTION
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil
	}

	// Check if this is a cmd-* unit
	if !strings.HasPrefix(fields[0], "cmd-") {
		return nil
	}

	// Extract unit name, state, and description
	unit := fields[0]
	state := fields[2] // ACTIVE column
	
	// Description is everything after the 4th field
	var description string
	if len(fields) > 4 {
		description = strings.Join(fields[4:], " ")
	}

	return &UnitInfo{
		Name:        unit,
		State:       state,
		Description: description,
	}
}

// parseUnits extracts unit information from systemctl output
func parseUnits(output string) []UnitInfo {
	var units []UnitInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	
	// Skip header lines (usually first line or two)
	headerPattern := regexp.MustCompile(`^(UNIT|●)`)
	inHeader := true
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		// Skip header/footer lines
		if inHeader && headerPattern.MatchString(line) {
			continue
		}
		inHeader = false
		
		// Skip footer lines
		if strings.Contains(line, "loaded units listed") || 
		   strings.Contains(line, "To show all") {
			break
		}
		
		if info := parseUnitLine(line); info != nil {
			units = append(units, *info)
		}
	}
	return units
}

// listCmdUnits lists all cmd-* units with detailed information
func listCmdUnits(verbose bool) error {
	out, err := runCmdCapture("systemctl", "--user", "--type=service", "--all", "--no-pager", "--no-legend")
	if err != nil && strings.TrimSpace(out) == "" {
		return fmt.Errorf("failed to query systemctl: %w", err)
	}

	units := parseUnits(out)
	if len(units) == 0 {
		fmt.Println(colorize(colorGray, "No cmd-* units found."))
		return nil
	}

	// Group by state
	byState := make(map[string][]UnitInfo)
	for _, u := range units {
		byState[u.State] = append(byState[u.State], u)
	}

	// Print grouped output
	stateOrder := []string{"running", "failed", "inactive", "dead"}
	stateColors := map[string]string{
		"running":  colorGreen,
		"failed":   colorRed,
		"inactive": colorYellow,
		"dead":     colorGray,
	}

	for _, state := range stateOrder {
		if unitList, ok := byState[state]; ok && len(unitList) > 0 {
			color := stateColors[state]
			if color == "" {
				color = colorReset
			}
			fmt.Printf("\n%s (%d):\n", colorize(color, strings.ToUpper(state)), len(unitList))
			for _, u := range unitList {
				if verbose && u.Description != "" {
					fmt.Printf("  %s - %s\n", u.Name, colorize(colorGray, u.Description))
				} else {
					fmt.Printf("  %s\n", u.Name)
				}
			}
		}
	}

	// Print any units with other states
	for state, unitList := range byState {
		isKnownState := false
		for _, known := range stateOrder {
			if state == known {
				isKnownState = true
				break
			}
		}
		if !isKnownState && len(unitList) > 0 {
			fmt.Printf("\n%s (%d):\n", strings.ToUpper(state), len(unitList))
			for _, u := range unitList {
				if verbose && u.Description != "" {
					fmt.Printf("  %s - %s\n", u.Name, colorize(colorGray, u.Description))
				} else {
					fmt.Printf("  %s\n", u.Name)
				}
			}
		}
	}

	return nil
}

// listCmdUnitsByState returns units in a specific state
func listCmdUnitsByState(state string) ([]string, error) {
	out, err := runCmdCapture("systemctl", "--user", "--type=service", "--state="+state, "--no-pager", "--no-legend")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("failed to query units in state '%s': %w", state, err)
	}
	
	units := parseUnits(out)
	var names []string
	for _, u := range units {
		names = append(names, u.Name)
	}
	return names, nil
}

// showUnitStatus displays status and recent logs for a unit
func showUnitStatus(unitName string, follow bool) error {
	// Ensure .service suffix
	if !strings.HasSuffix(unitName, ".service") {
		unitName = unitName + ".service"
	}

	// Show status
	fmt.Printf("%s\n", colorize(colorBlue, "=== Status for "+unitName+" ==="))
	if err := runCmdPassthru("systemctl", "--user", "status", unitName, "--no-pager"); err != nil {
		// status returns non-zero for inactive units, but we still want to show logs
		if !strings.Contains(err.Error(), "exit status") {
			return fmt.Errorf("failed to get status: %w", err)
		}
	}

	// Show recent logs
	fmt.Printf("\n%s\n", colorize(colorBlue, "=== Recent logs ==="))
	logArgs := []string{"--user", "-u", unitName, "-n", "50", "--no-pager"}
	if follow {
		logArgs = append(logArgs, "-f")
		fmt.Println(colorize(colorGray, "(Following logs, press Ctrl+C to exit)"))
	}
	
	if err := runCmdPassthru("journalctl", logArgs...); err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	return nil
}

// cleanUnits removes dead and failed units
func cleanUnits(autoYes bool) error {
	// Gather failed and inactive units
	failed, _ := listCmdUnitsByState("failed")
	inactive, _ := listCmdUnitsByState("inactive")
	dead, _ := listCmdUnitsByState("dead")
	
	seen := map[string]bool{}
	var toClean []string
	
	for _, units := range [][]string{failed, inactive, dead} {
		for _, u := range units {
			if !seen[u] {
				seen[u] = true
				toClean = append(toClean, u)
			}
		}
	}
	
	if len(toClean) == 0 {
		fmt.Println(colorize(colorGreen, "✓ No dead or failed cmd-* units to clean."))
		return nil
	}

	// Show what will be cleaned
	fmt.Printf("Found %d unit(s) to clean:\n", len(toClean))
	for _, u := range toClean {
		// Get state to show it
		out, _ := runCmdCapture("systemctl", "--user", "show", u, "-p", "ActiveState", "--value")
		state := strings.TrimSpace(out)
		stateColor := colorGray
		if state == "failed" {
			stateColor = colorRed
		}
		fmt.Printf("  %s %s\n", u, colorize(stateColor, "["+state+"]"))
	}

	// Confirm unless auto-yes
	if !autoYes {
		fmt.Print("\nClean these units? (y/N) ")
		var resp string
		fmt.Scanln(&resp)
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp != "y" && resp != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Clean units
	fmt.Println("\nCleaning units...")
	successCount := 0
	for _, u := range toClean {
		if out, err := runCmdCapture("systemctl", "--user", "reset-failed", u); err != nil {
			fmt.Fprintf(os.Stderr, "%s Failed to clean %s: %v\n", colorize(colorRed, "✗"), u, err)
			if strings.TrimSpace(out) != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", out)
			}
		} else {
			successCount++
		}
	}
	
	fmt.Printf("\n%s Cleaned %d/%d units\n", colorize(colorGreen, "✓"), successCount, len(toClean))
	return nil
}

// killUnit stops a specific unit
func killUnit(unitName string) error {
	// Ensure .service suffix
	if !strings.HasSuffix(unitName, ".service") {
		unitName = unitName + ".service"
	}

	// Check if unit exists and is running
	out, err := runCmdCapture("systemctl", "--user", "is-active", unitName)
	state := strings.TrimSpace(out)
	
	if err != nil && state != "activating" && state != "reloading" {
		return fmt.Errorf("unit '%s' is not running (state: %s)", unitName, state)
	}

	// Stop the unit
	if out, err := runCmdCapture("systemctl", "--user", "stop", unitName); err != nil {
		return fmt.Errorf("failed to stop %s: %w\n%s", unitName, err, out)
	}
	
	fmt.Printf("%s Stopped %s\n", colorize(colorGreen, "✓"), unitName)
	return nil
}

// killAllUnits stops all running cmd-* units
func killAllUnits(autoYes bool) error {
	running, err := listCmdUnitsByState("running")
	if err != nil {
		return fmt.Errorf("failed to query running units: %w", err)
	}

	if len(running) == 0 {
		fmt.Println(colorize(colorGreen, "✓ No running cmd-* units to kill."))
		return nil
	}

	// Show what will be killed
	fmt.Printf("Found %d running unit(s):\n", len(running))
	for _, u := range running {
		fmt.Printf("  %s\n", u)
	}

	// Confirm unless auto-yes
	if !autoYes {
		fmt.Print("\nStop ALL of these units? (y/N) ")
		var resp string
		fmt.Scanln(&resp)
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp != "y" && resp != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Kill units
	fmt.Println("\nStopping units...")
	successCount := 0
	for _, u := range running {
		if out, err := runCmdCapture("systemctl", "--user", "stop", u); err != nil {
			fmt.Fprintf(os.Stderr, "%s Failed to stop %s: %v\n", colorize(colorRed, "✗"), u, err)
			if strings.TrimSpace(out) != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", out)
			}
		} else {
			successCount++
		}
	}
	
	fmt.Printf("\n%s Stopped %d/%d units\n", colorize(colorGreen, "✓"), successCount, len(running))
	return nil
}

// startCommand starts a command as a transient systemd unit
func startCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}

	unit := fmt.Sprintf("cmd-%d", time.Now().Unix())
	fmt.Printf("Starting unit: %s\n", colorize(colorBlue, unit))
	fmt.Printf("Command: %s\n", colorize(colorGray, strings.Join(args, " ")))

	// Build systemd-run args
	sdArgs := []string{"--user", "--same-dir", "--unit=" + unit}
	sdArgs = append(sdArgs, args...)

	if err := runCmdPassthru("systemd-run", sdArgs...); err != nil {
		return fmt.Errorf("systemd-run failed: %w", err)
	}

	fmt.Printf("\n%s Unit started successfully\n", colorize(colorGreen, "✓"))
	fmt.Printf("View status with: %s\n", colorize(colorGray, os.Args[0]+" -s "+unit))
	return nil
}

func main() {
	// Custom usage function to show our help instead of default flag usage
	flag.Usage = func() {
		showHelp()
	}

	// Flags
	listFlag := flag.Bool("l", false, "List all cmd-* units grouped by state")
	listFlagLong := flag.Bool("list", false, "List all cmd-* units grouped by state")
	verboseFlag := flag.Bool("v", false, "Verbose output (show descriptions)")
	verboseFlagLong := flag.Bool("verbose", false, "Verbose output (show descriptions)")
	statusUnit := flag.String("s", "", "Show status and logs for a specific unit")
	statusUnitLong := flag.String("status", "", "Show status and logs for a specific unit")
	followLogs := flag.Bool("f", false, "Follow logs when showing status (use with -s)")
	followLogsLong := flag.Bool("follow", false, "Follow logs when showing status (use with -s)")
	cleanFlag := flag.Bool("clean", false, "Clean dead/failed/inactive cmd-* units")
	killUnitFlag := flag.String("k", "", "Stop specific running unit")
	killUnitLongFlag := flag.String("kill", "", "Stop specific running unit")
	killAllFlag := flag.Bool("K", false, "Kill all running cmd-* units")
	killAllLongFlag := flag.Bool("kill-all", false, "Kill all running cmd-* units")
	yesAll := flag.Bool("y", false, "Answer yes to all confirmations")
	yesAllLong := flag.Bool("yes", false, "Answer yes to all confirmations")
	noColorFlag := flag.Bool("no-color", false, "Disable colored output")
	helpFlag := flag.Bool("h", false, "Show help")
	helpFlagLong := flag.Bool("help", false, "Show help")

	flag.Parse()

	// Aggregate shorthand/longhand
	if *listFlagLong {
		*listFlag = true
	}
	if *verboseFlagLong {
		*verboseFlag = true
	}
	if *statusUnitLong != "" && *statusUnit == "" {
		*statusUnit = *statusUnitLong
	}
	if *followLogsLong {
		*followLogs = true
	}
	if *helpFlagLong {
		*helpFlag = true
	}
	if *killUnitLongFlag != "" && *killUnitFlag == "" {
		*killUnitFlag = *killUnitLongFlag
	}
	if *killAllLongFlag {
		*killAllFlag = true
	}
	if *yesAllLong {
		*yesAll = true
	}
	if *noColorFlag {
		useColor = false
	}

	// Show help
	if *helpFlag {
		showHelp()
		return
	}

	// Check systemd/tools available
	if err := checkSystemdAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", colorize(colorRed, "Error:"), err.Error())
		os.Exit(1)
	}
	if err := mustHaveCommand("systemctl"); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", colorize(colorRed, "Error:"), err.Error())
		os.Exit(1)
	}
	if err := mustHaveCommand("systemd-run"); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", colorize(colorRed, "Error:"), err.Error())
		os.Exit(1)
	}

	// Handle commands
	var err error
	switch {
	case *listFlag:
		err = listCmdUnits(*verboseFlag)

	case *statusUnit != "":
		err = showUnitStatus(*statusUnit, *followLogs)

	case *cleanFlag:
		err = cleanUnits(*yesAll)

	case *killAllFlag:
		err = killAllUnits(*yesAll)

	case *killUnitFlag != "":
		err = killUnit(*killUnitFlag)

	default:
		// Start mode: remaining args are the command
		args := flag.Args()
		if len(args) == 0 {
			showHelp()
			os.Exit(1)
		}
		err = startCommand(args)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", colorize(colorRed, "Error:"), err.Error())
		os.Exit(1)
	}
}
