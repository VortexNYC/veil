# Same binary. Streamable HTTP MCP. Not a Worker.
# No VOLUME: Railway rejects it. Vault is a Railway volume mounted at /data.
# Distroless has no shell. Do not put $PORT in CMD — mcp binds os.Getenv("PORT").
# Glue is the same image: start /identity-glue. It binds GLUE_LISTEN or :$PORT.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/veil ./cmd/veil
RUN CGO_ENABLED=0 go build -trimpath -o /out/identity-glue ./cmd/identity-glue

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/veil /veil
COPY --from=build /out/identity-glue /identity-glue
ENV VEIL_HOME=/data
EXPOSE 4461 4456
ENTRYPOINT ["/veil"]
CMD ["mcp"]
