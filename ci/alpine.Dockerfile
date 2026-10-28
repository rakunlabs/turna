ARG ALPINE=alpine:3.24.3

FROM $ALPINE

RUN apk --no-cache --no-progress add tzdata ca-certificates

COPY turna /
ENTRYPOINT [ "/turna" ]
