# Contributing

## Setup

```sh
git clone https://github.com/atakang7/axon
cd axon
go build ./...
go test ./...
```

## Guidelines

- Keep code minimal — less is more
- No unnecessary abstractions or comments
- Every change should have a test if it touches logic
- PRs should be focused — one thing per PR

## Submitting

1. Fork and create a branch
2. Make your change
3. `go test ./...` must pass
4. Open a PR with a clear description of what and why
