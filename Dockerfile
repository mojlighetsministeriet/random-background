# Run the build
FROM mojlighetsministeriet/go-polymer-faster-build
ENV WORKDIR /go/src/github.com/mojlighetsministeriet/random-background
COPY . $WORKDIR
WORKDIR $WORKDIR
RUN go get -t -v ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

# Create the final docker image
FROM scratch
COPY --from=0 /go/src/github.com/mojlighetsministeriet/random-background/random-background /
ENTRYPOINT ["/random-background"]
