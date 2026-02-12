# Contributing to Konta

Thank you for your interest in contributing to Konta!

## Getting Started

### Prerequisites
- Go 1.21+
- Docker
- Git

### Setup Development Environment

```bash
# Clone the repository
git clone https://github.com/kontacd/konta.git
cd konta

# Install dependencies
go mod download

# Build
make build

# Run tests
make test
```

## Development Workflow

1. **Fork the repository** on GitHub
2. **Create a feature branch**:
   ```bash
   git checkout -b feature/my-feature
   ```
3. **Make your changes**:
   - Keep changes focused
   - Add tests for new features
   - Update documentation
4. **Run tests**:
   ```bash
   make test
   ```
5. **Commit with clear messages**:
   ```bash
   git commit -m "feat: add new feature"
   ```
6. **Push to your fork**:
   ```bash
   git push origin feature/my-feature
   ```
7. **Create a Pull Request**

## Code Style

- Follow Go conventions (gofmt)
- Use clear, descriptive variable names
- Add comments for exported functions
- Keep functions small and focused
- Use meaningful commit messages

## Testing

Write tests for:
- New features
- Bug fixes
- Edge cases

Run tests:
```bash
make test
```

## Documentation

Update documentation when:
- Adding new features
- Changing behavior
- Fixing bugs
- Adding examples

## Areas for Contribution

### Features
- [ ] REST API endpoints
- [ ] Webhook support
- [ ] Prometheus metrics
- [ ] Multi-repo support
- [ ] Rollback commands
- [ ] More Git providers (GitLab, Gitea)

### Improvements
- [ ] Better error messages
- [ ] Performance optimizations
- [ ] Configuration validation
- [ ] Health checks
- [ ] Rate limiting
- [ ] Caching

### Documentation
- [ ] More examples
- [ ] Video tutorials
- [ ] Architecture docs
- [ ] Troubleshooting guide
- [ ] FAQ

### Testing
- [ ] Integration tests
- [ ] End-to-end tests
- [ ] Docker Compose edge cases
- [ ] Error scenarios

## Reporting Issues

When reporting bugs:
1. Use a clear, descriptive title
2. Provide a detailed description
3. Include reproduction steps
4. Share your environment (OS, Go version)
5. Include relevant logs

## Pull Request Process

1. Update the README.md with any new features
2. Add tests for new functionality
3. Ensure all tests pass: `make test`
4. Keep PR focused on a single feature/fix
5. Reference related issues
6. Wait for review and address feedback

## Code Review

All contributions are reviewed by maintainers. We look for:
- Code quality
- Test coverage
- Documentation
- Alignment with project goals

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Questions?

Feel free to:
- Open an issue for bugs/features
- Ask in discussions: https://github.com/kontacd/konta/discussions
- Email: support@kontacd.io

---

Happy contributing! 🚀
