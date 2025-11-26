# Background systemd-run helper

A small CLI wrapper around `systemd-run` to easily run background user services

Usage:
```
$ ./bgx -h                                                                         
bgx - Background systemd-run helper

Usage:
  bgx [options] <command...>    Start command as transient systemd user service
  bgx -l | --list               List all cmd-* units grouped by state
  bgx -s <unit> | --status      Show status and logs for a specific unit
  bgx --clean                   Clean dead/failed/inactive cmd-* units
  bgx -k <unit> | --kill        Stop specific running unit
  bgx -K | --kill-all           Stop all running cmd-* units

Options:
  -v, --verbose                Show detailed information (descriptions)
  -f, --follow                 Follow logs in real-time (use with -s)
  -y, --yes                    Answer yes to all confirmations
  --no-color                   Disable colored output
  -h, --help                   Show this help message

Examples:
  bgx gowitness report server
  bgx -l
  bgx -l -v
  bgx -s cmd-1732459032
  bgx -s cmd-1732459032 -f
  bgx -k cmd-1732459032
  bgx -K
  bgx --clean

Environment:
  NO_COLOR                     Disable colored output if set
```

Install:

```go
go install github.com/JakeFrost1412/bgx@latest
```
