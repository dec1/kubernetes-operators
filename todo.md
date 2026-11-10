# To do

- **Run the operator against a real cluster** (optional): [manifests/](manifests/readme.md) has the CRD, a sample Widget and the steps; not yet tried.
- **Deploy the operator inside a cluster** (optional, after the above): ServiceAccount and RBAC are in `manifests/`; still needed: a Dockerfile per variant and a Deployment.
- **Test the real HTTP API client**: needs a cluster (eg kind in CI) or a fake HTTP server.
- **Older material**: decide what to keep from `operator-api-interaction.md`; rename `example/` and its subfolders once the content has settled.
- **Python tooling** (before publishing): move `control_loop/python` to uv (`uv.lock`, pytest in a dev group), add Ruff and a type checker, and switch the CI job to `uv sync --locked` and `uv run --locked`. Until then, `pyproject.toml` holds only pytest settings.
