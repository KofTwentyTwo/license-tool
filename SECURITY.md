# Security Policy

## Supported Versions

| Version | Supported |
| ------- | --------- |
| 1.0.x   | yes       |
| 0.x     | no        |

## Reporting A Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please use one of the following methods:

1. **GitHub Security Advisories**: Create a private security advisory at
   `https://github.com/KofTwentyTwo/license-tool/security/advisories`

2. **PGP Encryption**: If you have the maintainer's PGP public key,
   encrypt your report and send it via email.

### What To Include

- Type of vulnerability (e.g., path traversal, command injection, file corruption)
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- Acknowledgment: within 48 hours
- Status update: within 7 days
- Resolution: within 30 days (or discussion of timeline if complex)

## Security Best Practices

### Safe By Default

- `apply` and `license` are dry-run by default and print a unified diff. Files are only
  mutated when `--write` is passed explicitly.
- Writes are atomic (temp-then-rename) and require a clean git working tree unless
  `--allow-dirty` is passed. Non-git directories require `--force`. Git is the undo.
- The tool never deletes file content other than a positively-identified license header
  block, and is idempotent: re-running never stacks or drifts headers.

### Secrets And Tokens

- The tool reads and rewrites source files and license metadata only. It does not read,
  store, or transmit credentials, and it never logs secrets.
- `HOMEBREW_TAP_TOKEN` and `GITHUB_TOKEN` are used only by the release workflow and are
  read from the CI secret store. They are never hardcoded in workflow files or committed.

### Optional Shell-Out

- Dependency-license resolution can optionally shell out to an ecosystem's native tool
  (`--resolve-deps tool`). This is opt-in. The default (`ondisk`) reads already-resolved
  on-disk metadata and runs no external commands.

## Known Security Considerations

- Running `apply --write` on an untrusted repository will rewrite that repository's
  license headers. Review the dry-run diff first.
- `--resolve-deps tool` executes ecosystem tooling found on `PATH`; only enable it for
  repositories you trust.
