package court

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsimiRuntimeRedirectsStdin verifies that __asimi_run redirects stdin from
// /dev/null so subcommands that read stdin get immediate EOF instead of blocking
// the persistent bash session. This is the core fix for edict 369.
func TestAsimiRuntimeRedirectsStdin(t *testing.T) {
	require.NotEmpty(t, dotagentsAsimiRuntime, "embedded asimi_runtime.sh must not be empty")
	assert.Contains(t, dotagentsAsimiRuntime, "</dev/null",
		"__asimi_run must redirect stdin from /dev/null to prevent blocking")
	assert.Contains(t, dotagentsAsimiRuntime, "__asimi_run()",
		"__asimi_run function must be defined")

	// Verify the redirect is inside the subshell, not just floating
	lines := strings.Split(dotagentsAsimiRuntime, "\n")
	var foundRedirect bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "eval") && strings.Contains(trimmed, "</dev/null") {
			foundRedirect = true
			break
		}
	}
	assert.True(t, foundRedirect,
		"the </dev/null redirect must be on the same line as eval inside __asimi_run")
}

// TestAsimiRuntimeExitCodePreserved verifies that __asimi_run captures the exit
// code from the subcommand and returns it, so the protocol markers are accurate.
func TestAsimiRuntimeExitCodePreserved(t *testing.T) {
	require.NotEmpty(t, dotagentsAsimiRuntime)
	assert.Contains(t, dotagentsAsimiRuntime, "local exit_code=$?",
		"__asimi_run must capture the exit code immediately after eval")
	assert.Contains(t, dotagentsAsimiRuntime, "return $exit_code",
		"__asimi_run must return the captured exit code")
}

// TestAsimiRuntimeProtocolMarkers verifies the stdout protocol markers that the
// podman runner parses are present and correctly formatted.
func TestAsimiRuntimeProtocolMarkers(t *testing.T) {
	require.NotEmpty(t, dotagentsAsimiRuntime)
	assert.Contains(t, dotagentsAsimiRuntime, "__ASIMI_STDOUT_START",
		"__asimi_run must emit __ASIMI_STDOUT_START marker")
	assert.Contains(t, dotagentsAsimiRuntime, "__ASIMI_STDOUT_END",
		"__asimi_run must emit __ASIMI_STDOUT_END marker")
}

// TestBashrcSetsGitTerminalPrompt verifies that bashrc exports GIT_TERMINAL_PROMPT=0
// so git operations fail with a clear credential error instead of attempting an
// interactive prompt that can never be satisfied in the sandbox.
func TestBashrcSetsGitTerminalPrompt(t *testing.T) {
	require.NotEmpty(t, dotagentsBashrc, "embedded bashrc must not be empty")
	assert.Contains(t, dotagentsBashrc, "export GIT_TERMINAL_PROMPT=0",
		"bashrc must export GIT_TERMINAL_PROMPT=0 to prevent interactive git prompts")
}

// TestBashrcSetsTermDumb verifies that TERM=dumb is set to prevent interactive
// terminal features in the sandbox.
func TestBashrcSetsTermDumb(t *testing.T) {
	require.NotEmpty(t, dotagentsBashrc)
	assert.Contains(t, dotagentsBashrc, `export TERM="dumb"`,
		"bashrc must export TERM=dumb for non-interactive sandbox")
}

// TestBashrcDoesNotRedefineAsimiRun verifies that bashrc does not redefine
// __asimi_run, which is installed into /etc/bash.bashrc by the Dockerfile.
func TestBashrcDoesNotRedefineAsimiRun(t *testing.T) {
	require.NotEmpty(t, dotagentsBashrc)
	// The comment explicitly says "Do not redefine __asimi_run below"
	assert.Contains(t, dotagentsBashrc, "Do not redefine __asimi_run",
		"bashrc must contain the comment prohibiting __asimi_run redefinition")
	assert.NotContains(t, dotagentsBashrc, "__asimi_run()",
		"bashrc must NOT define __asimi_run function")
}

// TestBashrcGitShimPresent verifies that the git function wrapper (Li enforcement)
// is present in bashrc to block dangerous git flags.
func TestBashrcGitShimPresent(t *testing.T) {
	require.NotEmpty(t, dotagentsBashrc)
	assert.Contains(t, dotagentsBashrc, "git()",
		"bashrc must define git() function wrapper for Li enforcement")
	assert.Contains(t, dotagentsBashrc, "--hard",
		"git shim must block --hard flag")
	assert.Contains(t, dotagentsBashrc, "--force",
		"git shim must block --force flag")
}

// TestDockerfileInstallsRuntimeCorrectly verifies that the Dockerfile installs
// asimi_runtime.sh into /etc/bash.bashrc and bashrc as /root/.bashrc.
func TestDockerfileInstallsRuntimeCorrectly(t *testing.T) {
	require.NotEmpty(t, dotagentsDockerfile)
	assert.Contains(t, dotagentsDockerfile, "asimi_runtime.sh /tmp/asimi_runtime.sh",
		"Dockerfile must COPY asimi_runtime.sh to /tmp")
	assert.Contains(t, dotagentsDockerfile, "cat /tmp/asimi_runtime.sh >> /etc/bash.bashrc",
		"Dockerfile must append asimi_runtime.sh to /etc/bash.bashrc")
	assert.Contains(t, dotagentsDockerfile, "bashrc /root/.bashrc",
		"Dockerfile must COPY bashrc to /root/.bashrc")
}
