# Development Environment Setup

To ensure a consistent dev env and to help you gain first-hand experience with containers, we simply provide a container specification as the basis for your dev env.

## Prerequisites
* Access to a *nix machine (Linux or MacOS).

## Steps
### Download and install the Docker Engine
### Start the Docker Engine
After installation and starting Docker, `docker version` from the commandline should show both the client and server versions.
### Run the dev container
```bash
docker run -it -v myhomedir:/home/nonroot git.cs426.cloud/labs/devenv:latest /bin/bash
```
Notes:
* `-it` starts the container in interactive mode, attaching the terminal.
* `-v myhomedir:/home/nonroot` mounts a docker volume named `myhomedir` to the home directory `/home/nonroot` inside the container. This volume is persistent across container restarts.

Once the container is running, you can work inside the container as you would on a regular terminal. The container is pre-configured with the necessary tools and dependencies.

### IMPORTANT: Back Up Your Work
Before you start working on the labs, it is advisable to figure out how to transfer files into and from the container to the host machine in order to back up your work. Refer to the docker documentations and try out the relevant commands.

## Optional
### Run in VS Code devcontainers
See `.devcontainer.json`.

### Building your own docker image
* You can build your own with `docker build -t my-dev-env .` The first build might take a while, but subsequent builds will be faster due to caching.
* To start the container and launch a bash shell, run `docker run -it -v myhomedir:/home/nonroot my-dev-env /bin/bash`
* We have provided the docker file so that you may add additional tools or dependencies as needed.

