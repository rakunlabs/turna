ARG ALPINE=alpine:3.21.2

FROM $ALPINE

RUN apk --no-cache --no-progress add tzdata ca-certificates

COPY turna /
ENTRYPOINT [ "/turna" ]
