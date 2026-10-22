# Working on the docs

This site is [Zensical](https://zensical.org), built with uv through the toolchain's go archetype. Its look is the
[flux-containers](https://oss.bitwisemedia.uk/flux-containers/) and
[flux-manifests](https://oss.bitwisemedia.uk/flux-manifests/) sites': the same `docs/stylesheets/extra.css` tokens and
header partials, with a landing page in `overrides/home.html`.

```sh
make serve       # http://localhost:8005, live reload for docs/ edits
make docs-build  # render into site/
```

## Layout

```text
zensical.toml              site config and the nav
docs/                      the pages
  stylesheets/extra.css    the BitWise Media Group palette and the landing styles
  assets/logo-mark.svg
overrides/
  home.html                the landing page (hero, marquee, resource cards)
  main.html, partials/     header and drawer lockups
pyproject.toml · uv.lock   the Zensical pin
```

The landing page reads its resource count and cards from the **Resources** section of the `nav` in `zensical.toml`
(every entry but its overview), and each card's blurb from that page's `description` front matter. A new kind needs only
its page and its line in the nav.

## Pages that embed live files

[Helm values](helm-values.md) includes the chart's `values.yaml` with `--8<-- "<path>"` (`pymdownx.snippets`), so the
listing can't fall out of date. Snippet paths resolve against `docs/` and the repository root (`base_path` in
`zensical.toml`). `check_paths` is on, so a page embedding a file that was moved or deleted fails the build.

## Publishing

The org's reusable release workflow (`release.yaml`) has a docs job that runs `uv run zensical build` and deploys the
result to GitHub Pages whenever a release is cut and a `zensical.toml` is present. The published site at
<https://oss.bitwisemedia.uk/dex-client-controller/> therefore describes the latest release. Pages must be set to
**Settings → Pages → Source → GitHub Actions**.
