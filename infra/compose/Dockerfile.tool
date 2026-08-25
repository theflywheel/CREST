# Local mocks (SMS gateway, payment rail) and one-shot utilities like
# tools/seed. Never deployed anywhere real.
#
# Same layer-sharing shape as Dockerfile.service: everything through the
# compile is ARG-free and identical across the tool builds, so BuildKit
# compiles once and each tool build is a copy.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./tools/mocks/... ./tools/seed
# Where the tool lives. The mocks by default; TOOLDIR=tools lets the same
# image build one-shot utilities like tools/seed for a deployed demo. Only the
# basename matters for selecting the compiled binary.
ARG TOOL
ARG TOOLDIR=tools/mocks
RUN cp /out/${TOOL} /out/tool

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tool /mock
# The fixture world, for tools that seed (tools/seed with CREST_WORLD set).
# A few KB; inert for the mocks.
COPY --from=build /src/tests/fixtures/world.yaml /fixtures/world.yaml
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mock"]
