// Package shell implements remux-shell, a login shell wrapper for terminal
// managers like herdr that launch a configurable shell for every pane.
//
// remux-shell finds the remux space containing the working directory, adds
// the space's env and execs the user's real shell. Outside a space, or on any
// error, it execs the real shell unchanged: it must never stop a shell from
// starting.
package shell

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/johanhenriksson/remux/git"
	"github.com/johanhenriksson/remux/registry"
	"github.com/johanhenriksson/remux/spaces"
)

// Name is the executable name (usually a symlink to remux) that runs the
// wrapper instead of the remux CLI.
const Name = "remux-shell"

// IsShell reports whether argv0 invokes remux as remux-shell. Login shells
// get a leading dash ("-remux-shell").
func IsShell(argv0 string) bool {
	return strings.TrimPrefix(filepath.Base(argv0), "-") == Name
}

// Main runs the wrapper and execs the real shell. It only returns if every
// exec failed.
func Main(argv0 string, args []string) {
	login := strings.HasPrefix(filepath.Base(argv0), "-")
	shell := resolveShell(os.Getenv, userShell, isSelf)

	env := os.Environ()
	if extra, err := safeSpaceEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "remux-shell: %v\n", err)
	} else {
		env = mergeEnv(env, extra)
	}
	// Terminal managers point SHELL at the wrapper; programs that spawn
	// $SHELL should get the real shell.
	env = mergeEnv(env, map[string]string{"SHELL": shell})

	argv := append([]string{shellArgv0(shell, login)}, args...)
	err := syscall.Exec(shell, argv, env)
	fmt.Fprintf(os.Stderr, "remux-shell: exec %s: %v\n", shell, err)
	err = syscall.Exec("/bin/sh", append([]string{shellArgv0("/bin/sh", login)}, args...), env)
	fmt.Fprintf(os.Stderr, "remux-shell: exec /bin/sh: %v\n", err)
	os.Exit(127)
}

// safeSpaceEnv returns the env of the space containing the working directory,
// or nil outside a space. Panics are turned into errors.
func safeSpaceEnv() (env map[string]string, err error) {
	defer func() {
		if r := recover(); r != nil {
			env, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil
	}
	return SpaceEnv(cwd)
}

// errNotSpace marks directories that don't belong to a remux space.
var errNotSpace = errors.New("not a remux space")

// SpaceEnv returns the session env of the remux space containing dir, or nil
// if dir isn't inside one.
func SpaceEnv(dir string) (map[string]string, error) {
	root, err := worktreeRoot(dir)
	if err != nil {
		return nil, nil
	}
	// Worktrees not managed by remux have no registry entry next to them
	reg, err := registry.Load(filepath.Dir(root))
	if err != nil || (reg.GetByPath(root) == nil && reg.Get(filepath.Base(root)) == nil) {
		return nil, nil
	}
	space, err := spaces.Open(root)
	if err != nil {
		return nil, err
	}
	return space.SessionEnv()
}

// worktreeRoot walks up from dir to the root of the linked git worktree that
// contains it. Main checkouts aren't remux spaces.
func worktreeRoot(dir string) (string, error) {
	dir = filepath.Clean(dir)
	for {
		info, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			if git.IsWorktree(dir) {
				return dir, nil
			}
			if info.IsDir() {
				return "", errNotSpace
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNotSpace
		}
		dir = parent
	}
}

// resolveShell picks the real shell: REMUX_SHELL, then $SHELL unless it
// points back at the wrapper, then the user's login shell, then a default.
func resolveShell(getenv func(string) string, userShell func() string, isSelf func(string) bool) string {
	for _, candidate := range []string{getenv("REMUX_SHELL"), getenv("SHELL"), userShell()} {
		if candidate != "" && !isSelf(candidate) && executable(candidate) {
			return candidate
		}
	}
	if runtime.GOOS == "darwin" && executable("/bin/zsh") {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func executable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

// isSelf reports whether path runs remux (directly or through a symlink).
func isSelf(path string) bool {
	if IsShell(path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	if selfResolved, err := filepath.EvalSymlinks(self); err == nil {
		self = selfResolved
	}
	return resolved == self
}

// userShell returns the login shell from the user database.
func userShell() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output()
		if err != nil {
			return ""
		}
		_, shell, _ := strings.Cut(strings.TrimSpace(string(out)), ":")
		return strings.TrimSpace(shell)
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) >= 7 && fields[0] == u.Username {
			return fields[6]
		}
	}
	return ""
}

// shellArgv0 returns argv[0] for the shell; a leading dash asks for a login
// shell, mirroring how the wrapper itself was started.
func shellArgv0(shell string, login bool) string {
	name := filepath.Base(shell)
	if login {
		return "-" + name
	}
	return name
}

// mergeEnv returns env with extra set, replacing existing keys.
func mergeEnv(env []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return env
	}
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if _, replaced := extra[key]; !replaced {
			out = append(out, kv)
		}
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+extra[k])
	}
	return out
}
