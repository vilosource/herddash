## What

<!-- What does this change do? -->

## Why

<!-- Why is this needed? Link an issue or ADR if one exists. -->

## How it was verified

<!-- Not "tests pass". What did you actually observe? For anything touching the herdr
     protocol or the live view, say which herdr version you ran against. -->

## Checklist

- [ ] `go build ./... && go test ./... && go vet ./...` pass locally
- [ ] `gofmt` clean
- [ ] Commits carry no AI attribution (no `Co-Authored-By: Claude`/similar, no "Generated with…", no 🤖)
- [ ] Commit messages / PR title follow [Conventional Commits](https://www.conventionalcommits.org/)
- [ ] If this adds a network surface or stores new data, the security implications are stated above
