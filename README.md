# sshtun

`sshtun` automates VPN point-to-point configuration of one or more
`tun` tunnel pairs using SSH as the secure transport layer. The CLI is
configured via a json file and is intended to run as a `systemd`
service. `sshtun` is written entirely in Go. Linux x86_64 (amd64) is
currently the only supported platform.

## Pre-requisites

* Linux x86_64 (amd64) on the local and remote host
* SSH server (i.e OpenSSH) running on the remote host
* `sshtun` need `root` privileges, preferrably via *setuid root* as it
  was designed or simply running as `root`
* SSH keys to remote hosts need to be un-encrypted (without a
  passphrase)
* The user on the remote host (`remote_user`) need to be able to run
  `sudo` without being prompted for a password

*If the remote host runs OpenSSH, `PermitTunnel` does not have to be
enabled as `sshtun` does not utilize OpenSSH tun tunneling.*

## Building and installing

Since `sshtun` contains an embedded helper binary (`tunreadwriter`),
it is not possible to execute `go install` to build and install
`sshtun`, instead, a `Makefile` is provided. The following command
will build the helper binary, `sshtun` itself, and install it under
`/usr/local/sbin` (prepends the `install` command with `sudo`)...

```consoltext
$ make clean install
```

## How

`sshtun` automates local and remote configuration of `tun` devices and
`systemd` unit file installation. Configuration of the remote `tun`
device and all traffic forwarding between the local and remote network
is handled by an internal binary being secure-copied (`scp`) to the
remote host via SSH. The internal binary (`tunreadwriter`) creates a
`tun` device, configures it with network address and mask, and then
links up the device. The helper binary does not alter firewall rules
or enable IP forwarding. If you want to use `sshtun` to establish a
set of link networks between a larger virtual network you will have to
write your own routing scripts.

Both `sshtun` and the remote traffic-forwarder (`tunreadwriter`) need
to run as a privileged user. `sshtun` is designed to run *setuid root*
and will escalate to the `root` user only when necessary while the
`tunreadwriter` is executed via `sudo` on the remote host through an
SSH session.

## Usage

```consoletext
$ sshtun -h
sshtun v0.0.0 (c) 2023 SA6MWA https://github.com/sa6mwa/sshtun
usage: bin/sshtun [options]
  -config file
        Configuration file as json (default "~/.config/sshtun/config.json")
  -edit
        Edit configuration json, implies -example if file does not exist
  -edit-unit
        Edit systemd unit, create a default if file does not exist
  -editor path
        Use path to edit configuration json or systemd unit
  -example
        Generate an example configuration if ~/.config/sshtun/config.json does not exist
  -install
        Install sshtun as a systemd service, use -edit-unit to generate an example unit
  -level string
        Set log level, can be DEBUG, INFO, WARN or ERROR (default "INFO")
  -systemctl path
        If issuing -install, path to systemctl (default "/usr/bin/systemctl")
  -systemd-unit path
        If issuing -install or -edit-unit, path to systemd unit file (default "/etc/systemd/system/sshtun.service")
  -uninstall
        Uninstall sshtun as a systemd service and remove unit file
```

Start by editing the configuration. A default configuration will be created for you.

```consoletext
$ sshtun -edit
```

The default configuration is located in `~/.config/sshtun/config.json`
and looks like this...

```json
{
  "tunnels": [
    {
      "name": "example",
      "protocol": "tcp4",
      "local_network": "172.18.0.1/24",
      "local_tun_device": "tun0",
      "local_mtu": 0,
      "remote": "localhost:22",
      "remote_network": "172.18.0.2/24",
      "remote_tun_device": "tun0",
      "remote_mtu": 0,
      "remote_user": "abc123",
      "use_ssh_agent": false,
      "private_key_files": [
        "~/.ssh/id_rsa"
      ],
      "remote_upload_directory": "",
      "remote_scp": "/usr/bin/scp",
      "enable": false,
      "keepalive_interval": "2m0s",
      "keepalive_max_error_count": 5
    }
  ]
}
```

Make necessary changes and set `enable` to `true` if you want to have
`sshtun` attempt to establish the specific tunnel.

To setup two tunnels, you would just add another configuration to the
`tunnels` slice...

```json
{
  "tunnels": [
    {
      "name": "example",
      "protocol": "tcp4",
      "local_network": "172.18.0.1/24",
      "local_tun_device": "tun0",
      "local_mtu": 0,
      "remote": "localhost:22",
      "remote_network": "172.18.0.2/24",
      "remote_tun_device": "tun0",
      "remote_mtu": 0,
      "remote_user": "abc123",
      "use_ssh_agent": false,
      "private_key_files": [
        "~/.ssh/id_rsa"
      ],
      "remote_upload_directory": "",
      "remote_scp": "/usr/bin/scp",
      "enable": true,
      "keepalive_interval": "2m0s",
      "keepalive_max_error_count": 5
    },
    {
      "name": "example2",
      "protocol": "tcp",
      "local_network": "172.19.0.1/24",
      "local_tun_device": "tun1",
      "remote": "localhost:22",
      "remote_network": "172.19.0.2/24",
      "remote_tun_device": "tun1",
      "remote_user": "abc123",
      "private_key_files": [
        "~/.ssh/id_rsa"
      ],
      "remote_upload_directory": "",
      "enable": true,
      "keepalive_interval": "30s",
      "keepalive_max_error_count": 5
    }
  ]
}
```

`sshtun` will start all tunnels in separate *goroutines*, but a mutex
lock prevents them from establishing more than one tunnel at a time
due the privilege escalation and de-escalation of the parent process.

When `keepalive_max_error_count` is reached, the SSH client is closed
which means the tunnel will also close and be re-established after a
couple of seconds (currently hard-coded to 5 seconds). If the count is
set to `0`, the tunnel will close on the first failed keepalive SSH
send request.

Starting `sshtun` is quite straight forward...

```consoletext
$ sshtun
{"time":"2023-10-13T00:51:50.609656457+02:00","level":"INFO","msg":"Welcome to sshtun v0.0.0 (c) 2023 SA6MWA https://github.com/sa6mwa/sshtun","config":"/home/abc123/.config/sshtun/config.json","total_tunnels":2,"enabled_tunnels":2}
{"time":"2023-10-13T00:51:50.60972869+02:00","level":"INFO","msg":"Connecting tunnel example","name":"example","remote":"farawaymachine:22","remote_net":"172.18.0.2/24","local_net":"172.18.0.1/24"}
{"time":"2023-10-13T00:51:50.609757097+02:00","level":"INFO","msg":"Connecting tunnel example2","name":"example2","remote":"anothermachine:22","remote_net":"172.19.0.2/24","local_net":"172.19.0.1/24"}
{"time":"2023-10-13T00:51:50.609797086+02:00","level":"INFO","msg":"Switching to uid 0","sudo":"ConfigureInterface","uid_to":0,"uid_from":1000,"name":"example2"}
{"time":"2023-10-13T00:51:50.610012138+02:00","level":"INFO","msg":"Creating local TUN device","tun":"tun1","name":"example2"}
{"time":"2023-10-13T00:51:50.610341531+02:00","level":"INFO","msg":"Configuring interface tun1 with address 172.19.0.1/24 and MTU 0","name":"example2","net":"172.19.0.1/24","mtu":0,"proto":"tcp"}
{"time":"2023-10-13T00:51:50.610486815+02:00","level":"INFO","msg":"Switching back to original uid","uid_to":1000,"uid_from":0,"name":"example2"}
{"time":"2023-10-13T00:51:50.611882706+02:00","level":"INFO","msg":"Connecting to ssh://anothermachine:22","remote":"anothermachine:22","name":"example2"}
{"time":"2023-10-13T00:51:50.905223993+02:00","level":"INFO","msg":"Uploading tunreadwriter as /tmp/tunreadwriter-20231012T225150-8296832003517942891 to ssh://anothermachine:22","name":"example2","tunreadwriter":"/tmp/tunreadwriter-20231012T225150-8296832003517942891","size":657060}
{"time":"2023-10-13T00:51:51.159613103+02:00","level":"INFO","msg":"Switching to uid 0","sudo":"LinkUp","uid_to":0,"uid_from":1000,"name":"example2"}
{"time":"2023-10-13T00:51:51.160190733+02:00","level":"INFO","msg":"Link up","local_tun":"tun1","local_net":"172.19.0.1/24","name":"example2"}
{"time":"2023-10-13T00:51:51.161464344+02:00","level":"INFO","msg":"Switching back to original uid","uid_to":1000,"uid_from":0,"name":"example2"}
{"time":"2023-10-13T00:51:51.162167867+02:00","level":"INFO","msg":"Enabling ssh keep-alive","keepalive_interval":"1m0s","keepalive_max_error_count":0,"name":"example2","remote":"anothermachine:22","remote_addr":"16.170.129.204:22","local_addr":"192.168.10.122:58954"}
{"time":"2023-10-13T00:51:51.162332488+02:00","level":"INFO","msg":"Starting tunnel","name":"example2","remote":"anothermachine:22","local_net":"172.19.0.1/24","remote_net":"172.19.0.2/24","local_tun":"tun1","remote_tun":"tun1","local_mtu":0,"remote_mtu":0}
{"time":"2023-10-13T00:51:51.162512442+02:00","level":"INFO","msg":"Switching to uid 0","sudo":"ConfigureInterface","uid_to":0,"uid_from":1000,"name":"example"}
{"time":"2023-10-13T00:51:51.162842818+02:00","level":"INFO","msg":"Creating local TUN device","tun":"tun0","name":"example"}
{"time":"2023-10-13T00:51:51.163152753+02:00","level":"INFO","msg":"Configuring interface tun0 with address 172.18.0.1/24 and MTU 0","name":"example","net":"172.18.0.1/24","mtu":0,"proto":"tcp4"}
{"time":"2023-10-13T00:51:51.163278167+02:00","level":"INFO","msg":"Switching back to original uid","uid_to":1000,"uid_from":0,"name":"example"}
{"time":"2023-10-13T00:51:51.169672818+02:00","level":"INFO","msg":"Connecting to ssh://farawaymachine:22","remote":"farawaymachine:22","name":"example"}
{"time":"2023-10-13T00:51:51.17656056+02:00","level":"INFO","msg":"Starting /tmp/tunreadwriter-20231012T225150-8296832003517942891 on remote ssh://farawaymachine:22","remote_addr":"16.170.129.204:22","remote":"farawaymachine:22","remote_command":"sudo /tmp/tunreadwriter-20231012T225150-8296832003517942891 -delete -dev tun1 -net 172.19.0.2/24 -mtu 0","name":"example2"}
{"time":"2023-10-13T00:51:51.461670033+02:00","level":"INFO","msg":"Uploading tunreadwriter as /tmp/tunreadwriter-20231012T225151-3649837345642611420 to ssh://farawaymachine:22","name":"example","tunreadwriter":"/tmp/tunreadwriter-20231012T225151-3649837345642611420","size":657060}
{"time":"2023-10-13T00:51:51.690043129+02:00","level":"INFO","msg":"Switching to uid 0","sudo":"LinkUp","uid_to":0,"uid_from":1000,"name":"example"}
{"time":"2023-10-13T00:51:51.690419437+02:00","level":"INFO","msg":"Link up","local_tun":"tun0","local_net":"172.18.0.1/24","name":"example"}
{"time":"2023-10-13T00:51:51.690887287+02:00","level":"INFO","msg":"Switching back to original uid","uid_to":1000,"uid_from":0,"name":"example"}
{"time":"2023-10-13T00:51:51.692298468+02:00","level":"INFO","msg":"Enabling ssh keep-alive","keepalive_interval":"2m0s","keepalive_max_error_count":5,"name":"example","remote":"farawaymachine:22","remote_addr":"16.170.129.204:22","local_addr":"192.168.10.122:58962"}
{"time":"2023-10-13T00:51:51.692404109+02:00","level":"INFO","msg":"Starting tunnel","name":"example","remote":"farawaymachine:22","local_net":"172.18.0.1/24","remote_net":"172.18.0.2/24","local_tun":"tun0","remote_tun":"tun0","local_mtu":0,"remote_mtu":0}
{"time":"2023-10-13T00:51:51.707026525+02:00","level":"INFO","msg":"Starting /tmp/tunreadwriter-20231012T225151-3649837345642611420 on remote ssh://farawaymachine:22","remote_addr":"16.170.129.204:22","remote":"farawaymachine:22","remote_command":"sudo /tmp/tunreadwriter-20231012T225151-3649837345642611420 -delete -dev tun0 -net 172.18.0.2/24 -mtu 0","name":"example"}
^C{"time":"2023-10-13T00:51:56.732345189+02:00","level":"WARN","msg":"Caught signal, shutting down","signal":"interrupt"}
{"time":"2023-10-13T00:51:56.732904098+02:00","level":"INFO","msg":"Tunnel closed","name":"example","remote":"farawaymachine:22","local_net":"172.18.0.1/24","remote_net":"172.18.0.2/24","local_tun":"tun0","remote_tun":"tun0","local_mtu":0,"remote_mtu":0}
{"time":"2023-10-13T00:51:56.732904101+02:00","level":"INFO","msg":"Tunnel closed","name":"example2","remote":"anothermachine:22","local_net":"172.19.0.1/24","remote_net":"172.19.0.2/24","local_tun":"tun1","remote_tun":"tun1","local_mtu":0,"remote_mtu":0}
```

When you have tested the tunnel on the command line, you can install it as a `systemd` service using the `-install` flag, but first you need to create a unit file. The `-edit-unit` option will create a default unit file under `/etc/systemd/system` called `sshtun.service`...

```consoletext
$ sshtun -edit-unit
```

The default unit file looks like this...

```systemdunit
[Unit]
Description=sshtun
After=network.target

[Service]
ExecStart=/usr/local/sbin/sshtun -config ~/.config/sshtun/config.json
Restart=on-failure
RestartSec=5s
WorkingDirectory=/tmp
StandardOutput=journal
StandardError=journal
User=abc123
Group=abc123

[Install]
WantedBy=multi-user.target
```

When you are done editing, you can start and enable the service using the `-install` option...

```consoletext
$ sshtun -install
{"time":"2023-10-13T01:05:04.457206498+02:00","level":"INFO","msg":"Installing systemd unit","file":"/etc/systemd/system/sshtun.service","systemctl":"/usr/bin/systemctl"}
{"time":"2023-10-13T01:05:04.84248159+02:00","level":"INFO","msg":"Systemd status","status":"● sshtun.service - sshtun\n     Loaded: loaded (/etc/systemd/system/sshtun.service; enabled; vendor preset: enabled)\n     Active: active (running) since Fri 2023-10-13 01:05:04 CEST; 12ms ago\n   Main PID: 121958 (sshtun)\n      Tasks: 1 (limit: 9196)\n     Memory: 256.0K\n        CPU: 0\n     CGroup: /system.slice/sshtun.service\n             └─121958 /home/sa6mwa/g/sshtun/bin/sshtun -config ~/.config/sshtun/config.json\n\nokt 13 01:05:04 greyskull systemd[1]: Started sshtun.\n","unit":"sshtun.service","file":"/etc/systemd/system/sshtun.service","systemctl":"/usr/bin/systemctl"}
```

Use `sudo journalctl -u sshtun` to look at the logs. To remove the
`sshtun` service, use the `-uninstall ` flag...

```consoletext
$ sshtun -uninstall
{"time":"2023-10-13T01:04:19.336094632+02:00","level":"INFO","msg":"Removing (uninstalling) systemd unit","file":"/etc/systemd/system/sshtun.service","systemctl":"/usr/bin/systemctl"}
```
