FROM alpine:latest

COPY collab-agent /collab-agent

RUN chmod 555 /collab-agent

ENTRYPOINT ["/collab-agent"]