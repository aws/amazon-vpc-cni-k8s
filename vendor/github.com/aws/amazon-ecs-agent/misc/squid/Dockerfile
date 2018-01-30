FROM alpine@sha256:fb9f16730ac6316afa4d97caa5130219927bfcecf0b0ce35c01dcb612f449739
RUN apk update && apk add squid
EXPOSE 3128
ENTRYPOINT ["squid", "-Nd", "1"]
