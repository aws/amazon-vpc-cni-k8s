The "scratch" docker image obviously does not contain any certificates. Since
the Amazon ECS Agent is build in scratch but makes heavy use of tls, it needs a
certificate store to trust.

This directory is meant to generate a store suitable for use by our agent.

Specifically, it uses debian's certificate store

