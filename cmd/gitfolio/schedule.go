package main

import (
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// The daily sync collects and sends at a set time (DESIGN 7.5); init asks about it, off by default.
const (
	launchdLabel = "team.aline.gitfolio"
	systemdUnit  = "gitfolio-sync"
	cronMarker   = "# gitfolio-schedule"
)

// winTaskName is the Windows Task Scheduler task; a variable so the Windows test uses its own.
var winTaskName = `GitFolio\Sync`

// cmdSchedule shows, sets (HH:MM, local time) or removes the daily sync.
func cmdSchedule(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		st, err := loadSync(dir)
		if err != nil {
			return err
		}
		when := tr(lang, "off")
		if cfg.Schedule != "" {
			when = cfg.Schedule
		}
		last := tr(lang, "never")
		if t, err := time.Parse(time.RFC3339, st.LastSync); err == nil {
			last = fmt.Sprintf(tr(lang, "lastSyncOK"), t.Local().Format("2006-01-02 15:04"))
			if st.LastError != "" {
				last = fmt.Sprintf(tr(lang, "lastSyncFailed"), t.Local().Format("2006-01-02 15:04"), st.LastError)
			}
		}
		say(lang, "scheduleStatus", when, last, tr(lang, map[bool]string{true: "on", false: "off"}[cfg.Deps && cfg.ScheduleDeps]))
		return nil
	}
	if args[0] == "deps" && len(args) == 2 && (args[1] == "on" || args[1] == "off") {
		cfg.ScheduleDeps = args[1] == "on"
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		if cfg.Schedule != "" { // registered before GITFOLIO_SCHEDULED existed: register it again
			h, m, _ := parseHHMM(cfg.Schedule)
			exe, err := gitfolioPath()
			if err != nil {
				return err
			}
			if _, err := schedule(dir, exe, h, m); err != nil {
				return err
			}
		}
		say(lang, "scheduleDeps", tr(lang, args[1]))
		if cfg.ScheduleDeps && !cfg.Deps {
			say(lang, "depsIsOff")
		}
		return nil
	}
	if len(args) > 1 {
		return failure("usage", "gitfolio schedule [HH:MM | off | deps on|off]")
	}
	if args[0] == "off" {
		return setSchedule(dir, lang, "")
	}
	return setSchedule(dir, lang, args[0])
}

// setSchedule registers the daily sync at when ("HH:MM", local time), or removes it when when is empty,
// and saves the choice. Callers hold the data lock.
func setSchedule(dir, lang, when string) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if when == "" {
		if err := unschedule(dir); err != nil {
			return err
		}
		cfg.Schedule = ""
		if err := saveConfig(dir, cfg); err != nil {
			return err
		}
		say(lang, "scheduleOff")
		return nil
	}
	h, m, ok := parseHHMM(when)
	if !ok {
		return failure("usage", "gitfolio schedule [HH:MM | off]")
	}
	exe, err := gitfolioPath()
	if err != nil {
		return err
	}
	logTo, err := schedule(dir, exe, h, m)
	if err != nil {
		return err
	}
	cfg.Schedule = fmt.Sprintf("%02d:%02d", h, m)
	if err := saveConfig(dir, cfg); err != nil {
		return err
	}
	msg := fmt.Sprintf(tr(lang, "scheduleOn"), cfg.Schedule, logTo)
	if runtime.GOOS == "darwin" {
		msg += tr(lang, "scheduleMacAccess") // seen on the first run for repositories in ~/Documents
	}
	show(os.Stdout, msg)
	return nil
}

func parseHHMM(s string) (h, m int, ok bool) {
	hh, mm, found := strings.Cut(s, ":")
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	return h, m, found && err1 == nil && err2 == nil && len(mm) == 2 && h >= 0 && h < 24 && m >= 0 && m < 60
}

// gitfolioPath is the command the scheduler runs: the one on PATH (a Homebrew link keeps working across
// upgrades), else this executable.
func gitfolioPath() (string, error) {
	if p, err := exec.LookPath("gitfolio"); err == nil {
		return filepath.Abs(p)
	}
	return os.Executable()
}

// scheduleEnv is what the scheduled run inherits from this shell: PATH, so git resolves as in a
// terminal, and GITFOLIO_LANG when set, so the log is in the same language. GITFOLIO_SCHEDULED tells
// sync it is the daily run (schedule deps).
func scheduleEnv() [][2]string {
	env := [][2]string{{"PATH", os.Getenv("PATH")}, {"GITFOLIO_SCHEDULED", "1"}}
	if v := os.Getenv("GITFOLIO_LANG"); v != "" {
		env = append(env, [2]string{"GITFOLIO_LANG", v})
	}
	return env
}

// schedule registers the daily sync with the OS scheduler and returns where its output goes.
func schedule(dir, exe string, h, m int) (string, error) {
	logFile := filepath.Join(dir, "schedule.log")
	switch runtime.GOOS {
	case "darwin":
		return logFile, launchdSchedule(exe, logFile, h, m)
	case "linux":
		serr := systemdSchedule(exe, h, m)
		if serr == nil {
			return "journalctl --user -u " + systemdUnit, cronSchedule("", "", -1, -1) // no older cron line running too
		}
		removeSystemd() // half set up: leave nothing behind before falling back to cron
		if _, err := exec.LookPath("crontab"); err != nil {
			return "", failure("scheduleNoScheduler", serr)
		}
		return logFile, cronSchedule(exe, logFile, h, m)
	case "windows":
		return logFile, taskSchedule(dir, exe, logFile, h, m)
	}
	return "", failure("scheduleUnsupported")
}

// unschedule removes whatever schedule registered; removing what is not there is not an error.
func unschedule(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		p := launchdPlistPath()
		exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), p).Run() // not loaded: fine
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	case "linux":
		removeSystemd()
		return cronSchedule("", "", -1, -1)
	case "windows":
		if exec.Command("schtasks", "/Query", "/TN", winTaskName).Run() != nil {
			return nil // not registered
		}
		if out, err := exec.Command("schtasks", "/Delete", "/TN", winTaskName, "/F").CombinedOutput(); err != nil {
			return fmt.Errorf("schtasks /Delete: %v %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// taskXML is a Windows Task Scheduler task that runs `gitfolio sync` every day at h:m through cmd, so its
// output goes to logFile; StartWhenAvailable runs a missed time at the next logon. It runs as the user,
// only while logged on, so no password is stored.
func taskXML(exe, logFile string, env [][2]string, h, m int, day time.Time) string {
	e := html.EscapeString
	set := ""
	for _, kv := range env {
		if kv[0] != "PATH" { // the task gets the user's own PATH
			set += `set "` + kv[0] + "=" + kv[1] + `" & `
		}
	}
	args := `/d /c "` + set + `"` + exe + `" sync >> "` + logFile + `" 2>&1"`
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>GitFolio daily sync to aline.team</Description>
  </RegistrationInfo>
  <Triggers>
    <CalendarTrigger>
      <StartBoundary>` + day.Format("2006-01-02") + fmt.Sprintf("T%02d:%02d:00", h, m) + `</StartBoundary>
      <Enabled>true</Enabled>
      <ScheduleByDay>
        <DaysInterval>1</DaysInterval>
      </ScheduleByDay>
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>cmd.exe</Command>
      <Arguments>` + e(args) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// taskSchedule registers the task with schtasks, replacing an older one. schtasks reads UTF-16 XML.
func taskSchedule(dir, exe, logFile string, h, m int) error {
	x := utf16.Encode([]rune(taskXML(exe, logFile, scheduleEnv(), h, m, time.Now())))
	b := []byte{0xFF, 0xFE} // little-endian byte order mark
	for _, u := range x {
		b = append(b, byte(u), byte(u>>8))
	}
	f := filepath.Join(dir, "schedule-task.xml")
	if err := os.WriteFile(f, b, 0o600); err != nil {
		return err
	}
	defer os.Remove(f)
	if out, err := exec.Command("schtasks", "/Create", "/TN", winTaskName, "/XML", f, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Create: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

// launchdPlist runs `gitfolio sync` every day at h:m. launchd runs a missed time when the Mac wakes up.
func launchdPlist(exe, logFile string, env [][2]string, h, m int) string {
	e := html.EscapeString
	vars := ""
	for _, kv := range env {
		vars += "\t\t<key>" + e(kv[0]) + "</key>\n\t\t<string>" + e(kv[1]) + "</string>\n"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + e(exe) + `</string>
		<string>sync</string>
	</array>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>` + strconv.Itoa(h) + `</integer>
		<key>Minute</key>
		<integer>` + strconv.Itoa(m) + `</integer>
	</dict>
	<key>EnvironmentVariables</key>
	<dict>
` + vars + `	</dict>
	<key>StandardOutPath</key>
	<string>` + e(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + e(logFile) + `</string>
</dict>
</plist>
`
}

func launchdSchedule(exe, logFile string, h, m int) error {
	p := launchdPlistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(launchdPlist(exe, logFile, scheduleEnv(), h, m)), 0o644); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	exec.Command("launchctl", "bootout", domain, p).Run() // an older schedule: replace it
	if out, err := exec.Command("launchctl", "bootstrap", domain, p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func systemdDir() string {
	base, err := os.UserConfigDir() // $XDG_CONFIG_HOME or ~/.config
	if err != nil {
		return ""
	}
	return filepath.Join(base, "systemd", "user")
}

// systemdUnits are a oneshot service and a daily timer; Persistent=true runs a missed time at the next boot.
func systemdUnits(exe string, env [][2]string, h, m int) (service, timer string) {
	esc := strings.NewReplacer("%", "%%", `"`, `\"`).Replace // % starts a systemd specifier
	service = "[Unit]\nDescription=GitFolio daily sync to aline.team\n\n[Service]\nType=oneshot\n"
	for _, kv := range env {
		service += `Environment="` + kv[0] + "=" + esc(kv[1]) + "\"\n"
	}
	service += `ExecStart="` + esc(exe) + "\" sync\n"
	timer = fmt.Sprintf(`[Unit]
Description=GitFolio daily sync to aline.team

[Timer]
OnCalendar=*-*-* %02d:%02d:00
Persistent=true

[Install]
WantedBy=timers.target
`, h, m)
	return service, timer
}

func systemdSchedule(exe string, h, m int) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return err
	}
	d := systemdDir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	service, timer := systemdUnits(exe, scheduleEnv(), h, m)
	if err := os.WriteFile(filepath.Join(d, systemdUnit+".service"), []byte(service), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d, systemdUnit+".timer"), []byte(timer), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", systemdUnit + ".timer"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func removeSystemd() {
	d := systemdDir()
	if _, err := os.Stat(filepath.Join(d, systemdUnit+".timer")); err != nil {
		return
	}
	exec.Command("systemctl", "--user", "disable", "--now", systemdUnit+".timer").Run()
	os.Remove(filepath.Join(d, systemdUnit+".timer"))
	os.Remove(filepath.Join(d, systemdUnit+".service"))
	exec.Command("systemctl", "--user", "daemon-reload").Run()
}

// cronSchedule puts one marked line in the user's crontab, replacing an older one; h < 0 only removes it.
func cronSchedule(exe, logFile string, h, m int) error {
	if _, err := exec.LookPath("crontab"); err != nil {
		if h < 0 {
			return nil
		}
		return failure("scheduleUnsupported")
	}
	current, err := exec.Command("crontab", "-l").Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && strings.Contains(string(ee.Stderr), "no crontab")) {
		return fmt.Errorf("crontab -l: %v", err) // never rewrite a crontab that could not be read
	}
	line := ""
	if h >= 0 {
		vars := ""
		for _, kv := range scheduleEnv() {
			vars += kv[0] + "=" + shellQuote(kv[1]) + " "
		}
		line = fmt.Sprintf("%d %d * * * %s%s sync >> %s 2>&1 %s", m, h, vars, shellQuote(exe), shellQuote(logFile), cronMarker)
	}
	next := cronWith(string(current), line)
	if next == string(current) {
		return nil
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(next)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cronWith returns crontab with GitFolio's line replaced by line (removed when line is empty).
func cronWith(crontab, line string) string {
	var out []string
	if crontab != "" {
		for _, l := range strings.Split(strings.TrimSuffix(crontab, "\n"), "\n") {
			if !strings.HasSuffix(l, cronMarker) {
				out = append(out, l)
			}
		}
	}
	if line != "" {
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
