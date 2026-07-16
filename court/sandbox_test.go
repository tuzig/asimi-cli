package court

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsimiRuntimeNotEmpty verifies that the embedded asimi_runtime.sh is present.
func TestAsimiRuntimeNotEmpty(t *testing.T) {
	require.NotEmpty(t, dotagentsAsimiRuntime, "embedded asimi_runtime.sh must not be empty")
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
