# Shell completions

Static completion scripts live in the top-level `completions/` directory and are
generated from the real flag registrations in
`internal/platform/{linux_cli.go,macos_cli.go}`:

```sh
python3 scripts/generate-completions.py          # regenerate
python3 scripts/generate-completions.py --check  # fail if stale
```

Files:

| Shell      | File                          | Install |
|------------|-------------------------------|---------|
| bash       | `nyist-connect.bash`          | source it, or install to `$(pkg-config --variable=completionsdir bash-completion)` |
| zsh        | `_nyist-connect`              | a directory in `$fpath`, then `compinit` |
| fish       | `nyist-connect.fish`          | `~/.config/fish/completions/` |
| PowerShell | `nyist-connect.ps1`           | dot-source it, or add it to your `$PROFILE` |

The scripts complete flag names and the documented values for `-network`
(`auto`, `nyist-tec`, `nyist-stu`) and `-autostart` (`enable`, `disable`,
`status`), plus file completion for `-config`. Go's `flag` package accepts both
`-flag` and `--flag`; bash, zsh and PowerShell offer both forms, while fish only
completes long options because `-o` there means a single-letter option.

When the maintainer-only archive is built (`BUILD_EXTRAS=1`), the files are
also copied into `authingnyist-packaging-extras-<version>.tar.gz` under
`completions/`. That archive is not published as a release asset.
