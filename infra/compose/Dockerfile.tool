# Local mocks (SMS gateway, payment rail). Never deployed anywhere real.
FROM golang:1.25-alpine AS build
ARG TOOL
# Where the tool lives. The mocks by default; TOOLDIR=tools lets the same
# image build one-shot utilities like tools/seed for a deployed demo.
ARG TOOLDIR=tools/mocks
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/tool ./${TOOLDIR}/${TOOL}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tool /mock
# The fixture world, for tools that seed (tools/seed with CREST_WORLD set).
# A few KB; inert for the mocks.
COPY --from=build /src/tests/fixtures/world.yaml /fixtures/world.yaml
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mock"]
