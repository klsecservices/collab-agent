# collab-agent

This is a module for collab2 that listens to necessary ports, collects requests and returns the required results.

For correct operation regardless of the environment, static compilation is performed in `build.sh`:
```
CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"'
```