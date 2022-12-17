FROM golang:1.19.2-alpine as builder 


# Pass a tag, branch or a commit using build-arg.  This allows a docker
# image to be built from a specified Git state.  The default image
# will use the Git tip of master by default.
ARG checkout="master"
ARG git_url="https://github.com/rkfg/regolancer.git"


# Install dependencies and build the binaries.
RUN apk add --no-cache  git \
    &&  git clone $git_url /go/src/github.com/regolancer \
    &&  cd /go/src/github.com/regolancer \
    &&  git checkout $checkout \
    &&  go install 


# Start a new, final image.
FROM alpine as final


RUN apk --no-cache add \
    bash \
    jq 


WORKDIR /app

COPY --from=builder /go/bin/regolancer /app




VOLUME [ "/root/.lnd" ]

ENTRYPOINT ["./regolancer"]

