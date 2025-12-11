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

Move items from `[Unreleased]` to a new version section:

```markdown
## [Unreleased]

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
git add -A && git commit -m "chore: releasing 0.2.1"
git tag -a v0.2.1 -m "Release 0.2.1"
git push origin main --tags
```


## See Also

- [RELEASE_CHECKLIST.md](../RELEASE_CHECKLIST.md) - Detailed Homebrew release checklist
- [CHANGELOG.md](../CHANGELOG.md) - Version history
