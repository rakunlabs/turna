ARG ALPINE=alpine:3.23.3

FROM $ALPINE

RUN apk --no-cache --no-progress add tzdata ca-certificates

COPY turna /
ENTRYPOINT [ "/turna" ]
