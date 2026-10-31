# Compatibility

## Hardware

Any equipment can be used, arm64 or amd64. The reference deployment runs on Raspberry Pi 4 boards with USB disks for Longhorn; its hardware is described in the [homelab instance](https://github.com/this-is-tobi/homelab/blob/main/docs/hardware.md).

## Operating System

Any Debian based system should work: tested with Debian 12 (bookworm) and Debian 13 (trixie), including `Raspberry Pi OS (64-bit)` upgraded in place to Debian 13 on the Debian generic arm64 kernel (the opt-in `kernel-upgrade` Ansible tag). In-place major version upgrades are supported via the `dist-upgrade` Ansible tag (see `Maintenance`).

For Raspberry, see :
- <https://www.raspberrypi.com/software>
- <https://www.raspberrypi.com/software/operating-systems>

> *To perform multiple OS installation in parallel on multiple devices take a look at [setup-pi.sh](../scripts/setup-pi.sh) script.*

## Access

Each target server __should have a static IP address__ to maintain consistency in the inventory file [hosts.yml](../ansible/inventory-example/hosts.yml), update this file with the appropriate values.

__SSH access to all machines is needed to install infrastructure__, username/password or ssh key could be used.

__Kubernetes admin access is needed to install services__, it use kubeconfig and the [run.sh](../run.sh) wrapper script will ask to confirm the context.
