# Contributing to the Amazon ECS CNI Plugins 

Contributions to the Amazon ECS CNI Plugins should be made via GitHub [pull
requests](https://github.com/aws/amazon-ecs-cni-plugins/pulls) and discussed using
GitHub [issues](https://github.com/aws/amazon-ecs-cni-plugins/issues).

### Before you start

If you would like to make a significant change, it's a good idea to first open
an issue to discuss it.

### Making the request

Development takes place against the `dev` branch of this repository and pull
requests should be opened against that branch.

### Testing

Any contributions should pass all tests, including those not run by our
current CI system.

You may run all tests by running the `make test` target (requires `go`,
and `go cover` to be installed).

## Licensing

The Amazon ECS CNI Plugins is released under an [Apache
2.0](http://aws.amazon.com/apache-2-0/) license. Any code you submit will be
released under that license.

For significant changes, we may ask you to sign a [Contributor License
Agreement](http://en.wikipedia.org/wiki/Contributor_License_Agreement).
