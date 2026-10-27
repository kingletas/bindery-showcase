package shellscan

import (
	"slices"
	"strings"
	"testing"
)

var opts = Options{Home: "/home/sample"}

func mustParse(t *testing.T, line string) *Scan {
	t.Helper()
	scan, err := Parse(line, opts)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", line, err)
	}
	return scan
}

// find returns the first invocation with the given name.
func find(scan *Scan, name string) (Invocation, bool) {
	for _, inv := range scan.Invocations {
		if inv.Known && inv.Name == name {
			return inv, true
		}
	}
	return Invocation{}, false
}

func texts(args []Arg) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a.Known {
			out[i] = a.Text
		} else {
			out[i] = "?"
		}
	}
	return out
}

func TestFindsACommandWhereverItSits(t *testing.T) {
	cases := map[string]string{
		"after and":           "cd src && make clean && rm -rf build",
		"after or":            "test -d x || rm -rf build",
		"after semicolon":     "echo hi; rm -rf build",
		"background":          "rm -rf build &",
		"in a substitution":   "echo $(rm -rf build)",
		"in backquotes":       "echo `rm -rf build`",
		"in a pipeline":       "ls | rm -rf build | cat",
		"in a subshell":       "(rm -rf build)",
		"in a block":          "{ rm -rf build; }",
		"in an if":            "if true; then rm -rf build; fi",
		"in a loop":           "for d in a b; do rm -rf build; done",
		"in a function":       "clean() { rm -rf build; }; clean",
		"in a case":           "case x in x) rm -rf build;; esac",
		"in a process subst":  "cat <(rm -rf build)",
		"in an assignment":    "x=$(rm -rf build)",
		"in an export":        "export x=$(rm -rf build)",
		"in a test":           "[[ $(rm -rf build) ]]",
		"after time":          "time rm -rf build",
		"in bash -c":          "bash -c 'rm -rf build'",
		"in sh -c":            "sh -c 'echo hi; rm -rf build'",
		"in bash -lc":         "bash -lc 'rm -rf build'",
		"in a nested -c":      "bash -c \"sh -c 'rm -rf build'\"",
		"in eval":             "eval 'rm -rf build'",
		"in a here-string":    "bash <<< 'rm -rf build'",
		"in a here-document":  "bash <<EOF\nrm -rf build\nEOF",
		"in a quoted heredoc": "sh <<'EOF'\nrm -rf build\nEOF",
		"in a heredoc body":   "cat <<EOF\n$(rm -rf build)\nEOF",
		"in a redirect word":  "echo hi > $(rm -rf build)",
		"in a pipe-all":       "echo hi |& rm -rf build",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			scan := mustParse(t, line)
			if _, ok := find(scan, "rm"); !ok {
				t.Fatalf("no rm found in %q; found %+v", line, scan.Invocations)
			}
			if len(scan.Unreadable) != 0 {
				t.Fatalf("%q reported unreadable parts: %v", line, scan.Unreadable)
			}
		})
	}
}

func TestLooksThroughWrappers(t *testing.T) {
	cases := map[string]string{
		"timeout":           "timeout 30 rm -rf build",
		"timeout options":   "timeout -s KILL -k 5 30 rm -rf build",
		"timeout long":      "timeout --signal=KILL --foreground 30 rm -rf build",
		"nice":              "nice rm -rf build",
		"nice -n":           "nice -n 10 rm -rf build",
		"nice -number":      "nice -10 rm -rf build",
		"ionice":            "ionice -c 3 rm -rf build",
		"env":               "env rm -rf build",
		"env assignments":   "env A=1 B=2 rm -rf build",
		"env -i":            "env -i -u X rm -rf build",
		"env -S":            "env -S 'rm -rf build'",
		"nohup":             "nohup rm -rf build",
		"setsid":            "setsid rm -rf build",
		"stdbuf":            "stdbuf -oL rm -rf build",
		"sudo":              "sudo rm -rf build",
		"sudo -u":           "sudo -u svc -E rm -rf build",
		"sudo --":           "sudo -- rm -rf build",
		"sudo -s":           "sudo -s 'rm -rf build'",
		"doas":              "doas -u svc rm -rf build",
		"command":           "command rm -rf build",
		"exec":              "exec rm -rf build",
		"builtin chain":     "sudo nice timeout 5 env A=1 rm -rf build",
		"a path":            "/usr/bin/rm -rf build",
		"a backslash":       "\\rm -rf build",
		"quoted name":       "'rm' -rf build",
		"dollar-quoted":     "$'rm' -rf build",
		"dollar hex escape": "$'\\x72m' -rf build",
		"dollar-quoted -c":  "bash -c $'rm -rf build'",
		"assignment prefix": "A=1 rm -rf build",
		"xargs":             "echo build | xargs rm -rf",
		"xargs options":     "echo build | xargs -n 1 -P 4 rm -rf",
		"xargs -I":          "echo build | xargs -I{} rm -rf {}",
		"xargs wrapped":     "echo build | xargs -0 nice rm -rf",
		"find -exec":        "find . -name build -exec rm -rf {} \\;",
		"find -execdir +":   "find . -name build -execdir rm -rf {} +",
		"find -exec sh -c":  "find . -exec sh -c 'rm -rf build' \\;",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			scan := mustParse(t, line)
			rm, ok := find(scan, "rm")
			if !ok {
				t.Fatalf("no rm found in %q; found %+v", line, scan.Invocations)
			}
			if !slices.Contains(texts(rm.Args), "-rf") {
				t.Fatalf("rm in %q has args %v, want -rf among them", line, texts(rm.Args))
			}
		})
	}
}

func TestDollarQuotedWordsAreDecoded(t *testing.T) {
	scan := mustParse(t, "git push --force origin $'ma\\x69n'")
	git, ok := find(scan, "git")
	if !ok {
		t.Fatal("no git found")
	}
	got := texts(git.Args)
	want := []string{"push", "--force", "origin", "main"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestAnOperandFedByXargsIsUnknown(t *testing.T) {
	scan := mustParse(t, "echo build | xargs rm -rf")
	rm, _ := find(scan, "rm")
	got := texts(rm.Args)
	if !slices.Equal(got, []string{"-rf", "?"}) {
		t.Fatalf("rm args = %v, want the flag and one unknown operand", got)
	}

	scan = mustParse(t, "echo build | xargs -I{} rm -rf {}")
	rm, _ = find(scan, "rm")
	if got := texts(rm.Args); !slices.Equal(got, []string{"-rf", "?"}) {
		t.Fatalf("rm args = %v, want the placeholder made unknown", got)
	}
}

func TestHomeAndTildeResolveOnlyWhenConfigured(t *testing.T) {
	scan := mustParse(t, `rm -rf ~/a "$HOME/b" ${HOME}/c`)
	rm, _ := find(scan, "rm")
	want := []string{"-rf", "/home/sample/a", "/home/sample/b", "/home/sample/c"}
	if got := texts(rm.Args); !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}

	scan, err := Parse(`rm -rf ~/a $HOME/b`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rm, _ = find(scan, "rm")
	if got := texts(rm.Args); !slices.Equal(got, []string{"-rf", "?", "?"}) {
		t.Fatalf("without a home, args = %v, want both unknown", got)
	}
}

func TestOtherExpansionsAreUnknownNeverEmpty(t *testing.T) {
	for _, word := range []string{`$X/data`, `${X:-/tmp}`, `${X}`, `$(pwd)`, "`pwd`", `$((1+1))`, `~root/x`, `{1..9}`} {
		scan := mustParse(t, "rm -rf "+word)
		rm, _ := find(scan, "rm")
		if len(rm.Args) < 2 || rm.Args[1].Known {
			t.Errorf("word %s was read as %v, want it unknown", word, texts(rm.Args))
		}
	}
}

func TestAnUnknownWordSaysWhetherItMaySplit(t *testing.T) {
	cases := map[string]bool{
		`$X`:          true,
		`${X}`:        true,
		`$(cat l)`:    true,
		"`cat l`":     true,
		`$((1+1))`:    true,
		`a$X`:         true,
		`{a,$X}`:      true,
		`"$@"`:        true,
		`"${arr[@]}"`: true,
		`"$X"`:        false,
		`"a$X"`:       false,
		`"$(cat l)"`:  false,
	}
	for word, want := range cases {
		scan := mustParse(t, "rm "+word)
		rm, _ := find(scan, "rm")
		if len(rm.Args) != 1 || rm.Args[0].Known {
			t.Errorf("word %s: args = %+v, want one unknown word", word, rm.Args)
			continue
		}
		if rm.Args[0].MaySplit != want {
			t.Errorf("word %s: MaySplit = %v, want %v", word, rm.Args[0].MaySplit, want)
		}
	}
}

func TestAnUnknownCommandIsReadAsAPossibleRunner(t *testing.T) {
	runs := map[string]string{
		"frobnicate rm -rf build":                 "rm",
		"frobnicate --fast /usr/bin/rm -rf build": "rm",
		"frobnicate -c 'cd x && rm -rf build'":    "rm",
		"frobnicate --cmd='rm -rf build'":         "rm",
		"frobnicate a b git push":                 "git",
		"frobnicate nice rm -rf build":            "nice",
		"alias x='rm -rf build'":                  "rm",
		"trap 'rm -rf build' EXIT":                "rm",
		"trap -- 'rm -rf build' INT":              "rm",
	}
	for line, want := range runs {
		if _, ok := find(mustParse(t, line), want); !ok {
			t.Errorf("%q: no %s found", line, want)
		}
	}
	data := []string{
		"echo rm -rf build",
		"printf '%s' rm",
		"frobnicate echo rm -rf build", // a words-only command ends the search
		"frobnicate 'sentence with rm'",
		"trap - EXIT",
		"trap -l",
		"alias ll='ls -la'",
	}
	for _, line := range data {
		scan := mustParse(t, line)
		if inv, ok := find(scan, "rm"); ok && len(inv.Args) > 0 && inv.Args[0].Text == "-rf" {
			t.Errorf("%q: rm -rf was found, but those words are data", line)
		}
	}
}

func TestAnAliasOrTrapBuiltFromAVariableIsUnreadable(t *testing.T) {
	for _, line := range []string{"alias x=$body", `trap "$action" EXIT`} {
		if scan := mustParse(t, line); len(scan.Unreadable) == 0 {
			t.Errorf("%q should report an unreadable part", line)
		}
	}
}

func TestBraceListsExpand(t *testing.T) {
	scan := mustParse(t, "rm -rf {a,b}/c")
	rm, _ := find(scan, "rm")
	if got := texts(rm.Args); !slices.Equal(got, []string{"-rf", "a/c", "b/c"}) {
		t.Fatalf("args = %v", got)
	}
}

func TestPipelineStagesAreTagged(t *testing.T) {
	scan := mustParse(t, "curl -s https://example.test/x | tee out | sh")
	curl, _ := find(scan, "curl")
	sh, _ := find(scan, "sh")
	if len(curl.Pipes) != 1 || len(sh.Pipes) != 1 {
		t.Fatalf("pipes: curl %v, sh %v", curl.Pipes, sh.Pipes)
	}
	if curl.Pipes[0].ID != sh.Pipes[0].ID || curl.Pipes[0].Stage != 0 || sh.Pipes[0].Stage != 2 {
		t.Fatalf("curl %v, sh %v: want one pipeline, stages 0 and 2", curl.Pipes, sh.Pipes)
	}
}

func TestAWrappedShellKeepsItsPipelineStage(t *testing.T) {
	scan := mustParse(t, "curl -s https://example.test/x | sudo env A=1 bash")
	bash, ok := find(scan, "bash")
	if !ok || len(bash.Pipes) != 1 || bash.Pipes[0].Stage != 1 {
		t.Fatalf("bash = %+v", bash)
	}
}

func TestSubstitutionsAreListedOnTheWordThatHoldsThem(t *testing.T) {
	scan := mustParse(t, `bash -c "$(curl -s https://example.test/x)"`)
	bash, _ := find(scan, "bash")
	if len(bash.Args) != 2 || len(bash.Args[1].Inner) == 0 || bash.Args[1].Inner[0].Name != "curl" {
		t.Fatalf("bash args = %+v", bash.Args)
	}
	if len(scan.Unreadable) == 0 {
		t.Fatal("a -c string built at run time should be unreadable")
	}
}

func TestInputRedirectionsAreListed(t *testing.T) {
	scan := mustParse(t, "grep key < sample.txt")
	grep, _ := find(scan, "grep")
	if len(grep.Inputs) != 1 || grep.Inputs[0].Text != "sample.txt" {
		t.Fatalf("inputs = %+v", grep.Inputs)
	}
}

func TestLinesItCannotParseAreErrors(t *testing.T) {
	cases := map[string]string{
		"unclosed quote":      `echo "abc`,
		"unclosed substitute": `echo $(rm -rf build`,
		"dangling pipe":       `ls |`,
		"stray fi":            `fi`,
		"zsh parameter flags": `echo ${(k)map}`,
		"zsh nested":          `print ${${(f)"$(cat x)"}[1]}`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if scan, err := Parse(line, opts); err == nil {
				t.Fatalf("Parse(%q) = %+v, want an error", line, scan)
			}
		})
	}
}

func TestPartsItCannotReadAreReported(t *testing.T) {
	cases := map[string]string{
		"a variable as the command":     `$tool -rf build`,
		"a substitution as the command": `$(which rm) -rf build`,
		"a -c string from a variable":   `bash -c "$script"`,
		"eval of a variable":            `eval "$script"`,
		"a heredoc with a variable":     "bash <<EOF\nrm -rf $dir\nEOF",
		"a shell reading a pipe":        `echo 'rm -rf build' | sh`,
		"a shell reading a pipe, -s":    `echo 'rm -rf build' | bash -s`,
		"a shell told to read stdin":    `echo 'rm -rf build' | bash /dev/stdin`,
		"a shell told to read fd 0":     `echo 'rm -rf build' | sh /dev/fd/0`,
		"a shell given dash":            `echo 'rm -rf build' | bash -`,
		"a nested string that is bad":   `bash -c 'echo "abc'`,
		"wrapper options from variable": `timeout $secs rm -rf build`,
		"xargs options from a variable": `echo x | xargs $opts rm`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			scan, err := Parse(line, opts)
			if err != nil {
				return // refusing at parse time is also fail-closed
			}
			if len(scan.Unreadable) == 0 {
				t.Fatalf("Parse(%q) reported nothing unreadable: %+v", line, scan.Invocations)
			}
		})
	}
}

// nest wraps a command in n levels of bash -c.
func nest(command string, n int) string {
	for i := 0; i < n; i++ {
		command = "bash -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
	}
	return command
}

func TestCommandStringsNestedTooDeeplyAreReported(t *testing.T) {
	shallow := mustParse(t, nest("rm -rf build", 3))
	if _, ok := find(shallow, "rm"); !ok || len(shallow.Unreadable) != 0 {
		t.Fatalf("three levels should be read: %+v", shallow)
	}
	deep := mustParse(t, nest("rm -rf build", maxDepth+2))
	if len(deep.Unreadable) == 0 {
		t.Fatal("a line nested past the limit should report an unreadable part")
	}
}

func TestReadableShellUseIsNotReportedUnreadable(t *testing.T) {
	cases := []string{
		"ls -la",
		"bash script.sh",
		"bash -c 'echo hi'",
		"sh",
		"echo hi | grep h | wc -l",
		"cat file.txt | sh -c 'wc -l'",
		"command -v rm",
		"echo \"home is $HOME\"",
		"find . -name '*.o' -print",
		"echo a | xargs echo",
	}
	for _, line := range cases {
		scan := mustParse(t, line)
		if len(scan.Unreadable) != 0 {
			t.Errorf("%q reported unreadable parts: %v", line, scan.Unreadable)
		}
	}
}

func TestCommandDashVIsALookupNotARun(t *testing.T) {
	scan := mustParse(t, "command -v rm")
	if _, ok := find(scan, "rm"); ok {
		t.Fatal("command -v rm only looks rm up; it should not list rm as run")
	}
}

func TestParseShellArgs(t *testing.T) {
	arg := func(s ...string) []Arg {
		out := make([]Arg, len(s))
		for i, t := range s {
			out[i] = Arg{Text: t, Known: true}
		}
		return out
	}
	cases := []struct {
		args    string
		command string
		hasCmd  bool
		stdin   bool
	}{
		{"", "", false, true},
		{"-c echo", "echo", true, false},
		{"-lc echo", "echo", true, false},
		{"-o pipefail -c echo", "echo", true, false},
		{"-e -c echo", "echo", true, false},
		{"-- script.sh", "", false, false},
		{"script.sh", "", false, false},
		{"-s", "", false, true},
		{"-s -- a b", "", false, true},
		{"+x", "", false, true},
		{"--norc", "", false, true},
		{"-c", "", false, false},
		{"-", "", false, true},
		{"/dev/stdin", "", false, true},
		{"/dev/fd/0", "", false, true},
		{"/proc/self/fd/0", "", false, true},
		{"-x /dev/stdin", "", false, true},
		{"script.sh /dev/stdin", "", false, false}, // the second operand is the script's argument
		{"-c echo /dev/stdin", "echo", true, false},
	}
	for _, c := range cases {
		sa := ParseShellArgs(arg(strings.Fields(c.args)...))
		if sa.HasCommand != c.hasCmd || sa.Command.Text != c.command || sa.ReadsStdin != c.stdin {
			t.Errorf("args %q: got %+v, want command %q (%v), stdin %v", c.args, sa, c.command, c.hasCmd, c.stdin)
		}
	}
}
