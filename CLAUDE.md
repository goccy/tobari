# CLAUDE.md

## Go Error Handling Style

- Keep error variables in the shortest possible scope. Prefer `if err := f(); err != nil` over declaring `err` separately.
- Always use `err` as the variable name for errors.
- When combining multiple errors, use `errors.Join`.

## Documentation Rules

- When modifying the `tobari view` command, always update `docs/cli/view.md` accordingly.
