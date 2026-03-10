# Release Management

This document describes the release process for Asimi CLI.

## Version Numbering

We follow [Semantic Versioning](https://semver.org/):

- **0.x.y** - Initial development; anything may change at any time
- **1.0.0+** - Public API is stable:
  - **MAJOR** (x.0.0) - Breaking changes
  - **MINOR** (2.x.0) - New features, backward compatible
  - **PATCH** (1.1.x) - Bug fixes, backward compatible

## Release Process

The instructions below use 0.2.1 as the version, please replace with the current version.

### 1. Prepare CHANGELOG.md

Review the git log since the last version and ensure all user notable changes are 
under the Unreleased section of the changle log.
Then, rename the section to 0.2.1 and add the date:

```markdown

## [0.2.1] - 2025-01-15

### Added
- New feature X

### Fixed
- Bug Y
```

### 2. Update Version in main.go

Edit line 34 in `main.go`:

```go
var version = "0.2.1"
```

### 3. Update README.md Roadmap

Replace completed roadmap items with new ones:

1. Remove issues that are now closed
2. Add new planned features from [open issues](https://github.com/afittestide/asimi-cli/issues)
3. Keep the table format:

```markdown
| Feature | Description |
|---------|-------------|
| [#XX - Feature Name](https://github.com/afittestide/asimi-cli/issues/XX) | Brief description |
```

### 4. Commit the Release

```bash
git add CHANGELOG.md main.go README.md
git commit -m "chore: releasing 0.2.1"
```

### 5. Tag and Push

```bash
git tag -a v0.2.1 -m "Release 0.2.1"
git push origin v0.2.1
```

This triggers the GitHub Actions release workflow which:
- Builds binaries for all platforms
- Creates a GitHub release
- Updates the Homebrew formula

## Post-Release

### Verify the Installer

Test the one-liner installer:
```bash
curl -fsSL https://asimi.dev/installer | bash
# or
curl -fsSL https://raw.githubusercontent.com/afittestide/asimi-cli/main/scripts/install.sh | bash
```

### Verify the Release

1. Check [GitHub Releases](https://github.com/afittestide/asimi-cli/releases)
2. Verify binaries are attached
3. Test Homebrew installation:
   ```bash
   brew upgrade asimi
   asimi --version
   ```

### Announce (Optional)

- GitHub Discussions
- Social media
- Community channels

## Quick Reference

```bash
# Full release flow
vim CHANGELOG.md                           # Polish changelog
sed -i 's/version = .*/version = "0.2.1"/' main.go  # Update version
vim README.md                              # Update roadmap
git add CHANGELOG.md main.go README.md && git commit -m "chore: releasing 0.2.1"
git tag -a v0.2.1 -m "Release 0.2.1"
git push origin main --tags
```


## One-Liner Installer

The one-liner installer (`scripts/install.sh`) is available for direct installation of Asimi.

### Hosting

The installer should be accessible at `https://asimi.dev/installer`. There are several options:

1. **GitHub Raw URL (Fallback)**: The script is always available at:
   ```
   https://raw.githubusercontent.com/afittestide/asimi-cli/main/scripts/install.sh
   ```

2. **Domain Redirect**: Configure `asimi.dev/installer` to redirect to the GitHub raw URL.

3. **Static Hosting**: Host the script on a CDN or static site (Netlify, Vercel, GitHub Pages).

### Updating the Installer

When making changes to the installer:
1. Edit `scripts/install.sh`
2. Test locally: `bash scripts/install.sh`
3. Commit and push to main
4. The GitHub raw URL updates automatically

### Installer Options

Users can customize installation via environment variables:
- `ASIMI_INSTALL_DIR` - Custom installation directory
- `ASIMI_VERSION` - Install a specific version (e.g., `v0.2.0`)
- `ASIMI_NO_MODIFY_PATH` - Skip automatic PATH modification

Example:
```bash
ASIMI_VERSION=v0.2.0 ASIMI_INSTALL_DIR=~/bin curl -fsSL https://asimi.dev/installer | bash
```

## See Also

- [RELEASE_CHECKLIST.md](../RELEASE_CHECKLIST.md) - Detailed Homebrew release checklist
- [CHANGELOG.md](../CHANGELOG.md) - Version history
