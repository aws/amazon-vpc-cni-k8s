FROM debian@sha256:8b1fc3a7a55c42e3445155b2f8f40c55de5f8bc8012992b26b570530c4bded9e

RUN apt-get update && apt-get install -y \
    python2.7 curl

RUN curl -O https://bootstrap.pypa.io/get-pip.py

RUN python2.7 get-pip.py && pip install awscli
