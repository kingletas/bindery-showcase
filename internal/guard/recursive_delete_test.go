package guard

import (
	"strings"
	"testing"
)

func TestRecursiveDeleteIsSilentOnHarmlessCommands(t *testing.T) {
	assertSilent(t, RecursiveDelete{}, testConfig(),
		"ls -la",
		"rm file.txt",
		"rm -f /tmp/outside.txt",             // not recursive
		"rm -rf build",                       // inside the root
		"rm -rf ./build/cache",               // inside the root
		"rm -rf /home/sample/work/app/dist",  // absolute, inside the root
		"rm -rf ~/work/app/dist",             // tilde, inside the root
		"rm -rf $HOME/work/app/dist",         // $HOME, inside the root
		"rm -rf build/*",                     // children of a directory in the root
		"rm -rf *",                           // children of the root itself
		"rm -rf /home/sample/work/app/*.log", // glob inside the root
		"rm -r -- -odd-name",                 // an operand that looks like a flag
		"rm -rf",                             // no operand at all
		"find . -name '*.o' -print",
		"find . -name '*.o' -delete", // the start path is the root, matches are inside
		"find build -delete",
		"echo build | xargs echo",
		"timeout 5 rm -rf build",
		"sudo -u svc rm -rf build",
		"bash -c 'rm -rf build'",
		"echo rm -rf /",           // an argument to echo, not a command
		"grep -r 'rm -rf /' docs", // a pattern, not a command
		"git rm -r --cached build",
		"command -v rm",
	)
}

func TestRecursiveDeleteRefusesAProtectedOrOutsidePath(t *testing.T) {
	cases := map[string][]string{
		"the filesystem root": {
			"rm -rf /", "rm -r /", "rm -rf /*", "rm -Rf /", "rm -fr /", "rm --recursive --force /",
			"rm -rf -- /", "rm -rf //", "rm -rf /.",
		},
		"the home directory": {
			"rm -rf ~", "rm -rf ~/", "rm -rf $HOME", "rm -rf ${HOME}", `rm -rf "$HOME"`,
			"rm -rf /home/sample", "rm -rf ~/*", "rm -rf /home",
		},
		"the workspace root itself": {
			"rm -rf .", "rm -rf ./", "rm -rf /home/sample/work/app", "rm -rf ../app", "rm -rf /home/sample/work",
		},
		"outside the workspace": {
			"rm -rf /tmp/other", "rm -rf ../sibling", "rm -rf /home/sample/documents",
			"rm -rf ~/documents", "rm -rf /var/lib/example", "rm -rf ../*",
		},
		"after another command": {
			"make clean && rm -rf /", "true || rm -rf ~", "echo done; rm -rf /tmp/other",
			"cd build && rm -rf /", "(rm -rf /)", "{ rm -rf /; }", "if true; then rm -rf /; fi",
			"clean() { rm -rf /; }; clean", "echo hi |& rm -rf /",
		},
		"inside a substitution": {
			"echo $(rm -rf /)", "echo `rm -rf /`", "x=$(rm -rf /)", "cat <(rm -rf /)",
		},
		"inside a pipeline": {
			"ls | rm -rf /", "echo x | rm -rf / | cat",
		},
		"inside a shell command string": {
			"bash -c 'rm -rf /'", "sh -c 'echo hi; rm -rf /'", "bash -lc 'rm -rf /'",
			"bash -c \"sh -c 'rm -rf /'\"", "eval 'rm -rf /'", "zsh -c 'rm -rf /'",
			"bash <<< 'rm -rf /'", "bash <<EOF\nrm -rf /\nEOF",
		},
		"behind a wrapper": {
			"timeout 30 rm -rf /", "timeout -s KILL 30 rm -rf /", "nice rm -rf /", "nice -n 5 rm -rf /",
			"ionice -c 3 rm -rf /", "env rm -rf /", "env A=1 rm -rf /", "env -i rm -rf /", "env -S 'rm -rf /'",
			"nohup rm -rf /", "setsid rm -rf /", "stdbuf -oL rm -rf /", "sudo rm -rf /", "sudo -u svc rm -rf /",
			"sudo -s 'rm -rf /'", "doas rm -rf /", "command rm -rf /", "exec rm -rf /",
			"sudo nice timeout 5 env A=1 rm -rf /", "time rm -rf /",
		},
		"behind xargs or find": {
			"echo / | xargs rm -rf", "echo / | xargs -n 1 -P 4 rm -rf", "echo / | xargs -I{} rm -rf {}",
			"echo / | xargs -0 nice rm -rf", "find / -name x -exec rm -rf {} \\;", "find . -execdir rm -rf {} +",
			"find / -delete", "find ~ -name '*.o' -delete", "find /tmp -delete",
		},
		"written as another spelling": {
			"/bin/rm -rf /", "\\rm -rf /", "'rm' -rf /", `"rm" -rf /`, "$'rm' -rf /", "$'\\x72m' -rf /",
			"rm -rf $'/'", "rm -rf $'\\x2f'", "rm -rf /'tmp'/other", "rm -rf {/,x}", "r\\m -rf /",
		},
	}
	for name, commands := range cases {
		t.Run(name, func(t *testing.T) {
			assertRefuses(t, RecursiveDelete{}, testConfig(), commands...)
		})
	}
}

func TestRecursiveDeleteRefusesATargetItCannotResolve(t *testing.T) {
	assertRefuses(t, RecursiveDelete{}, testConfig(),
		"rm -rf $BUILD_DIR",
		"rm -rf $BUILD_DIR/out",
		"rm -rf ${BUILD_DIR:-build}",
		"rm -rf $(pwd)",
		"rm -rf `dirname $0`",
		"rm -rf ~other/data",
		"rm -rf build/*(.)", // a zsh glob qualifier parses as an extended pattern
		"rm -rf {1..99999}",
		"cd /tmp && rm -rf build", // a relative path after cd starts somewhere unknown
		"pushd /tmp; rm -rf build",
	)
}

func TestRecursiveDeleteReasonNamesTheTarget(t *testing.T) {
	reason := RecursiveDelete{}.Check(shellInput(t, testConfig(), "rm -rf /tmp/other"))
	if !strings.Contains(reason, `"/tmp/other"`) || !strings.Contains(reason, "outside the workspace root") {
		t.Fatalf("reason = %q", reason)
	}
	reason = RecursiveDelete{}.Check(shellInput(t, testConfig(), "rm -rf /"))
	if !strings.Contains(reason, "filesystem root") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestRecursiveDeleteUsesTheCallsDirectory(t *testing.T) {
	cfg := testConfig()
	in := shellInput(t, cfg, "rm -rf build")
	if reason := (RecursiveDelete{}).Check(in); reason != "" {
		t.Fatalf("inside the root: %s", reason)
	}
	in.Call.Cwd = "/srv/elsewhere"
	if reason := (RecursiveDelete{}).Check(in); reason == "" {
		t.Fatal("the same relative path resolved in another directory should be refused")
	}
}

func TestRecursiveDeleteWithNoConfiguredRootRefusesEveryDelete(t *testing.T) {
	cfg := testConfig()
	cfg.WorkspaceRoots = nil
	assertRefuses(t, RecursiveDelete{}, cfg, "rm -rf build")
}

func TestRecursiveDeleteHonoursSeveralRoots(t *testing.T) {
	cfg := testConfig()
	cfg.WorkspaceRoots = []string{"/home/sample/work/app", "/srv/scratch"}
	assertSilent(t, RecursiveDelete{}, cfg, "rm -rf /srv/scratch/run1")
	assertRefuses(t, RecursiveDelete{}, cfg, "rm -rf /srv/scratch", "rm -rf /srv/other/run1")
}

func TestRecursiveDeleteIgnoresACallThatIsNotShell(t *testing.T) {
	in := Input{Call: Call{ToolName: "Read"}, Config: testConfig()}
	if reason := (RecursiveDelete{}).Check(in); reason != "" {
		t.Fatalf("a call with no shell scan should pass, got %q", reason)
	}
}
