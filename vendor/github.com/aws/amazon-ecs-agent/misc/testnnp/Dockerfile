# Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

FROM gcc:4.9
COPY . /usr/src/
WORKDIR /usr/src/
RUN gcc -o testnnp testnnp.c
RUN chmod +s ./testnnp
# At the time of this writing:
# ECS is not accepting --user=1000 (only accepts ^([a-z_][a-z0-9_-]{0,30})). 
# So overriding USER here
USER 1000
CMD ["./testnnp"]
