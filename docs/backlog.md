# Backlog

- Move local development secrets out of the root `.env` file into a managed secrets app.
- Keep `.env.example` in the repository as a non-secret template with required keys and safe placeholder values.
- Document how developers pull secrets locally and hydrate their `.env` without committing credentials.
- Transfer some envs to github secrets in pr.yml docker stage
- Goose migrations currently are doing natively. Move goose migrations to the container.
- Add TLS - in NewOrchestratorClient new client is created w/out credentials. Need to add TLS config and it's usage in grpc.NewClient