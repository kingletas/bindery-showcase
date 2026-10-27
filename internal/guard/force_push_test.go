package guard

import (
	"strings"
	"testing"
)

func TestForcePushIsSilentOnHarmlessCommands(t *testing.T) {
	assertSilent(t, ForcePush{}, testConfig(),
		"git status",
		"git push",
		"git push origin main",
		"git push -u origin main",
		"git push origin feature/login",
		"git push --force origin feature/login",
		"git push -f origin feature/login",
		"git push --force-with-lease origin feature/login",
		"git push origin +feature/login",
		"git push origin HEAD:feature/login --force",
		"git push --delete origin feature/login",
		"git push origin :feature/login",
		"git push --tags --force origin",
		"git -C ../other push origin main",
		"git log --oneline main",
		"git commit --amend -m 'fix' && git push origin feature/login",
		"echo git push --force origin main",
		"git fetch --force origin main",
		"git branch -f main HEAD~1",
		"bash -c 'git push origin feature/login'",
	)
}

func TestForcePushRefusesAProtectedBranch(t *testing.T) {
	cases := map[string][]string{
		"a force flag": {
			"git push --force origin main",
			"git push -f origin main",
			"git push origin main --force",
			"git push --force-with-lease origin main",
			"git push --force-with-lease=main:abc123 origin main",
			"git push --force-if-includes origin main",
			"git push -fu origin main",
			"git push --force origin master",
			"git push --force origin refs/heads/main",
			"git push --force origin HEAD:main",
			"git push --force origin HEAD:refs/heads/main",
			"git push --force origin feature:main",
			"git push -o ci.skip --force origin main",
			"git push --force -- origin main",
		},
		"a plus refspec": {
			"git push origin +main",
			"git push origin +HEAD:main",
			"git push origin +feature:refs/heads/main",
			"git push origin feature +main",
		},
		"a delete": {
			"git push --delete origin main",
			"git push -d origin main",
			"git push origin :main",
			"git push origin :refs/heads/master",
		},
		"git's global options": {
			"git -C ../repo push --force origin main",
			"git -c core.editor=true push --force origin main",
			"git --no-pager push --force origin main",
			"git --git-dir=.git push --force origin main",
		},
		"after another command": {
			"git add . && git commit -m x && git push --force origin main",
			"true || git push -f origin main",
			"echo $(git push -f origin main)",
			"git fetch; git push -f origin main",
			"(git push -f origin main)",
			"ls | git push -f origin main",
		},
		"a shell command string": {
			"bash -c 'git push --force origin main'",
			"sh -c \"git push -f origin main\"",
			"eval 'git push -f origin main'",
		},
		"a wrapper": {
			"timeout 60 git push --force origin main",
			"nice git push --force origin main",
			"env GIT_TRACE=1 git push --force origin main",
			"sudo -u svc git push --force origin main",
			"nohup git push --force origin main",
			"xargs git push --force origin main",
			"echo x | xargs -I{} git push --force origin main",
			"find . -maxdepth 0 -exec git push --force origin main \\;",
		},
		"another spelling": {
			"/usr/bin/git push --force origin main",
			"\\git push --force origin main",
			"git push --force origin $'main'",
			"git push --force origin $'ma\\x69n'",
			"git push --force origin 'main'",
			`git push "--force" origin "main"`,
			"git $'push' --force origin main",
		},
		"a branch that is not named": {
			"git push --force",
			"git push -f origin",
			"git push --force origin HEAD",
			"git push --force origin $BRANCH",
			"git push --force origin $(git branch --show-current)",
			"git push --force --all origin",
			"git push --mirror origin",
		},
	}
	for name, commands := range cases {
		t.Run(name, func(t *testing.T) {
			assertRefuses(t, ForcePush{}, testConfig(), commands...)
		})
	}
}

func TestForcePushUsesTheConfiguredBranches(t *testing.T) {
	cfg := testConfig()
	cfg.ProtectedBranches = []string{"release/*", "trunk"}
	assertRefuses(t, ForcePush{}, cfg,
		"git push --force origin trunk",
		"git push --force origin release/1.2",
	)
	assertSilent(t, ForcePush{}, cfg,
		"git push --force origin main", // not protected under this config
		"git push --force origin release",
	)
}

func TestForcePushReasonNamesTheBranch(t *testing.T) {
	reason := ForcePush{}.Check(shellInput(t, testConfig(), "git push -f origin refs/heads/main"))
	if !strings.Contains(reason, `"main"`) || !strings.Contains(reason, "Force-pushing") {
		t.Fatalf("reason = %q", reason)
	}
	reason = ForcePush{}.Check(shellInput(t, testConfig(), "git push origin :main"))
	if !strings.Contains(reason, "Deleting") {
		t.Fatalf("reason = %q", reason)
	}
}
