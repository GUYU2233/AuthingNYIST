# Homebrew Cask and Formula

Two different artifacts are generated from a completed release. Neither is
published as a release asset; they are maintainer output for self-hosted taps.

* `Casks/nyist-connect.rb` installs the macOS app from the two DMGs.
* `authingnyist.rb` is a Linux-only Formula for the portable CLI archive.

Generate both from the repository (`BUILD_EXTRAS=1` on the packaging script), or directly:

```sh
python3 packaging/homebrew/generate_cask.py \
  --version 1.2.3 --tag v1.2.3 --dist dist \
  --output packaging/homebrew/Casks/nyist-connect.rb
python3 scripts/generate-recipes.py --version v1.2.3 --dist dist \
  --output dist/package-recipes
```

The cask targets the modern Homebrew 4.x Cask DSL: `on_intel` and `on_arm`
blocks each carry their own `sha256` and `url`, so a single `version` serves
both disk images. It declares `app "NYIST Connect.app"` and exposes the CLI that
ships inside the bundle as `nyist-connect` via a `binary` stanza.

The app is ad-hoc signed but not notarized. The cask includes a caveat telling
users to allow the first launch in System Settings. Homebrew audit, installation,
and Gatekeeper behaviour must be confirmed on a real macOS machine; the offline
tests only validate the generated structure, hashes, and Ruby syntax.

Install a locally generated cask with:

```sh
brew install --cask ./packaging/homebrew/Casks/nyist-connect.rb
```

The Formula no longer supports macOS: the cask supersedes it there. Keeping both
would install the same CLI twice.
