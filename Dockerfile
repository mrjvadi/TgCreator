# Build only the modules a workflow needs, e.g.:
#   docker build --build-arg TAGS="no_mysql no_postgres" -t tgcreator .
# (`tgcreator compose --build .` fills TAGS automatically.)
FROM golang:1.25-alpine AS build
ARG TAGS=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /data && CGO_ENABLED=0 go build -trimpath -tags "$TAGS" -ldflags "-s -w" -o /out/tgcreator ./cmd/tgcreator

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tgcreator /usr/local/bin/tgcreator
# Writable by the nonroot user (sqlite databases, uploaded files).
COPY --from=build --chown=65532:65532 /data /data
WORKDIR /app
ENTRYPOINT ["/usr/local/bin/tgcreator"]
CMD ["run", "-w", "/app/workflow.json"]
