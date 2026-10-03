# Deploying the static Go web app as a systemd user service

**Target:** AlmaLinux 10, systemd 257, one long-running static Go HTTP binary,
TCP port 5554, SQLite database at ~/data/pushups.db.

**Research date:** 2026-08-24.

The examples below assume that the binary reads LISTEN_ADDR and DB_PATH.
If this binary exposes flags instead, keep the unit structure and replace the
two Environment= lines with the corresponding arguments on ExecStart=.
The application must create or open the database, but the parent ~/data
directory is created during installation.

## 1. Recommended user unit

Install this as ~/.config/systemd/user/pushup-counter.service:

~~~ini
[Unit]
Description=Pushup counter web app

[Service]
# exec reports an ExecStart/execve failure as a failed start. It does not
# claim readiness after the HTTP listener is bound; use Type=notify only when
# the program implements sd_notify (see below).
Type=exec
ExecStart=%h/.local/bin/pushup-counter
WorkingDirectory=%h

# %h is the home directory of the user running the user manager. Environment=
# performs systemd specifier expansion; it does not perform $HOME expansion.
Environment=LISTEN_ADDR=127.0.0.1:5554
Environment=DB_PATH=%h/data/pushups.db

Restart=on-failure
RestartSec=2s

# SIGTERM is the normal graceful-stop contract for Go's http.Server.
KillSignal=SIGTERM
TimeoutStopSec=30s

# Keep the SQLite file and any files the program creates private by default.
UMask=0077

# Be explicit about the journald stream transport.
StandardOutput=journal
StandardError=journal
SyslogIdentifier=pushup-counter

# These three are usable in ordinary --user services. The address-family
# list leaves room for local Unix sockets and IPv4/IPv6 TCP, including DNS.
NoNewPrivileges=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=default.target
~~~

Type=exec is a good default for a foreground binary: simple considers the
service started immediately after the manager forks, while exec waits until
the configured executable has actually been executed. Neither mode promises
that application initialization or ListenAndServe has completed; that is the
reason to use Type=notify only when the program really sends READY=1.
[systemd.service(5), systemd 257](https://www.freedesktop.org/software/systemd/man/257/systemd.service.html)

Restart=on-failure restarts crashes, non-zero exits, unexpected signals, and
operation timeouts, but a manager-requested stop is not restarted. A
RestartSec=2s delay avoids a tight crash loop. The service restart policy is
documented in [systemd.service(5), Restart=](https://www.freedesktop.org/software/systemd/man/257/systemd.service.html#Restart=).

Do not add User= or Group= for a normal non-root user unit: the per-user
manager already runs the service as that account, and a non-root user manager
cannot switch its service to another identity. [systemd.exec(5), user/group
identity](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html#User/Group%20Identity)

### Hardening directives: what actually works with --user

The important distinction is between restrictions implemented with seccomp or
process flags, and restrictions that need the service manager to create a
privileged filesystem namespace.

| Directive | Recommendation for this user unit | Caveat |
| --- | --- | --- |
| NoNewPrivileges=yes | Keep it. | Prevents privilege gain through execve, including set-user-ID/set-group-ID bits and file capabilities. It is available to user services. |
| MemoryDenyWriteExecute=yes | Keep it for a normal static Go server. | It blocks writable-plus-executable mappings and related executable shared-memory operations. It can break a JIT, plugin loader, or unusual runtime; test if the binary uses one. It is implemented with kernel process/seccomp mechanisms and is not a filesystem-namespace directive. |
| RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 | Keep it if the app only needs local IPC and TCP. | It is an allow-list enforced through the socket-address-family restriction. Add a family only when the app demonstrably needs it; for example, do not omit AF_UNIX if a library uses local IPC. |
| PrivateTmp=yes | Do not put it in the portable baseline. | It needs filesystem namespacing. Upstream permits this class of setting in a user manager only when the required unprivileged user namespace support is available, generally with PrivateUsers= in effect. On a host where unprivileged user namespaces are disabled or restricted, the unit can fail during namespace setup. |
| ProtectSystem=strict (or full) | Do not put it in the portable baseline. | It is generally unavailable to user services because filesystem namespacing requires privileges. It can work with PrivateUsers=yes and user namespaces, but then it needs careful ReadWritePaths= exceptions for the SQLite directory and testing of every path the app uses. |

The upstream manual explicitly says that filesystem-namespacing settings such as
ProtectSystem= are generally unavailable to per-user services, while noting
that many can work with PrivateUsers=true if unprivileged user namespaces are
available. The same restriction applies to PrivateTmp=. That is why the
baseline above uses the process/seccomp restrictions and does not pretend that
the familiar system-service hardening block is portable to systemctl --user.
[systemd.exec(5), sandboxing](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html#Sandboxing)

If the host has deliberately enabled unprivileged user namespaces and you want
the extra isolation, test a drop-in such as:

~~~ini
[Service]
PrivateUsers=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=%h/data
~~~

Treat this as an opt-in experiment, not as a guaranteed AlmaLinux user-unit
recipe. ProtectSystem=strict can also block future state/configuration files
under the home directory, and PrivateUsers changes the identity view inside the
namespace. Verify the resulting unit and application behavior before keeping
it.

### StateDirectory= versus the required plain path

StateDirectory= takes relative directory names, creates them when the unit
starts, and sets $STATE_DIRECTORY to their absolute path. For a user manager,
the directory is under $XDG_STATE_HOME; the XDG default is
$HOME/.local/state. [systemd.exec(5), automatic directories](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html#RuntimeDirectory=,%20StateDirectory=,%20CacheDirectory=,%20LogsDirectory=,%20ConfigurationDirectory=)
[and the XDG Base Directory specification](https://specifications.freedesktop.org/basedir/latest/)

Therefore this is the correct systemd-managed alternative if the database may
move to the XDG state location:

~~~ini
[Service]
StateDirectory=pushup-counter
StateDirectoryMode=0700
Environment=DB_PATH=%S/pushup-counter/pushups.db
~~~

StateDirectory= names must be relative and systemd creates the directory;
StateDirectoryMode=0700 makes the innermost directory private. %S is the
user-manager state-directory root, resolving to $XDG_STATE_HOME. This is clean
because systemd creates the directory and the path is not tied to shell home
syntax. It is not the requested ~/data/pushups.db, though, so the main unit
uses the explicit plain path %h/data/pushups.db. Do not configure both
approaches for the same database.
[systemd.exec(5), automatic directories](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html#RuntimeDirectory=,%20StateDirectory=,%20CacheDirectory=,%20LogsDirectory=,%20ConfigurationDirectory=)
[systemd.unit(5), specifiers](https://www.freedesktop.org/software/systemd/man/257/systemd.unit.html#Specifiers)

## 2. loginctl enable-linger

Check the current state as the target user:

~~~sh
loginctl show-user "$USER" --property=Linger
loginctl show-user "$USER" --property=Linger --value
~~~

Enable lingering (normally an administrator-authorized operation):

~~~sh
sudo loginctl enable-linger "$USER"
loginctl show-user "$USER" --property=Linger --value
~~~

The last command should print yes. To undo it:

~~~sh
sudo loginctl disable-linger "$USER"
~~~

For a named account, replace "$USER" with the exact login name, for example
sudo loginctl enable-linger exampleuser.

Linger tells systemd-logind to spawn that user's user@UID.service manager at
boot and keep it after the user logs out. It enables long-running user services
without an active login; it does not itself enable or start this particular
unit. The unit still needs to be enabled in the user manager with
WantedBy=default.target.
[loginctl(1), enable-linger](https://www.freedesktop.org/software/systemd/man/257/loginctl.html#enable-linger%20%5BUSER%E2%80%A6%5D)

## 3. Direct bind or socket activation?

Use a single service that binds directly to 127.0.0.1:5554.

Socket activation is useful when systemd should own the listening socket before
the application starts, when a service should be demand-started, when several
workers share one endpoint, or when preserving the listening socket across
restarts is a meaningful requirement. It adds a .socket unit and requires the
Go program to consume systemd's inherited file descriptor rather than call
ListenAndServe on its own address.
[systemd.socket(5)](https://www.freedesktop.org/software/systemd/man/257/systemd.socket.html)

This app is one small, always-on process on a non-privileged port. Direct bind
has fewer moving parts and makes the app's HTTP server and graceful-shutdown
path conventional. Do not add a .socket unit while the Go program also calls
ListenAndServe on :5554; they would compete for the same port. If the
requirement changes, github.com/coreos/go-systemd/v22/activation is the
corresponding Go integration package. [go-systemd activation package](https://pkg.go.dev/github.com/coreos/go-systemd/v22/activation)

If the app later needs socket activation, the shape is a separate user socket
unit such as this, paired with an application that accepts inherited listeners:

~~~ini
[Unit]
Description=Pushup counter listening socket

[Socket]
ListenStream=127.0.0.1:5554
Service=pushup-counter.service

[Install]
WantedBy=sockets.target
~~~

That is an alternative design, not part of the recommended deployment here.

## 4. Graceful shutdown and sd_notify

### SIGTERM contract

KillSignal=SIGTERM is explicit in the unit even though SIGTERM is systemd's
default. With no ExecStop=, systemd sends that signal when stopping the
service; if the service has not exited by TimeoutStopSec=, the manager sends
the final kill signal (normally SIGKILL). The default kill mode is the service
control group, which is appropriate for a single foreground Go process.
[systemd.kill(5)](https://www.freedesktop.org/software/systemd/man/257/systemd.kill.html)

TimeoutStopSec=30s is a practical bound for this small app. The Go program
should use a slightly shorter internal deadline, such as 25 seconds, so it has
time to return and close the database before systemd's hard deadline:

1. Catch SIGTERM (and SIGINT for interactive use) with signal.NotifyContext.
2. Stop accepting new HTTP connections with http.Server.Shutdown.
3. Let in-flight handlers finish within the internal deadline.
4. Close the SQLite database and other resources, then return from main.
5. Treat http.ErrServerClosed from ListenAndServe as the expected shutdown
   result, not as a crash.

http.Server.Shutdown closes listeners, closes idle connections, and waits for
active connections to become idle until the supplied context expires. It does
not close or wait for hijacked connections such as WebSockets; an application
using those must close them separately.
[Go net/http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)

signal.NotifyContext cancels its returned context when SIGTERM arrives and
changes the default signal behavior, so the program has a chance to perform
the orderly shutdown. Call the returned stop function when signal handling is
no longer needed.
[Go os/signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)

Do not use os.Exit in the normal SIGTERM path before cleanup completes, and do
not replace graceful shutdown with http.Server.Close unless the deadline has
expired and an immediate close is intentional.

### Is Type=notify worth it?

Not for this small app unless another unit depends on actual readiness after
database setup and listener binding. Type=exec already gives useful start
failure reporting, while Type=notify adds a protocol dependency and will
remain in the activating state until the program sends READY=1.

If accurate readiness or watchdog integration becomes valuable, use the
well-maintained Go bindings in github.com/coreos/go-systemd/v22/daemon (the
current v22 package documentation lists v22.7.0 in 2026) and change the unit
to:

~~~ini
[Service]
Type=notify
NotifyAccess=main
TimeoutStartSec=15s
~~~

After the binary has completed initialization and is ready to serve, send:

~~~go
_, err := daemon.SdNotify(false, daemon.SdNotifyReady)
~~~

The package exposes SdNotifyReady (READY=1) and SdNotifyStopping
(STOPPING=1).
[go-systemd daemon package](https://pkg.go.dev/github.com/coreos/go-systemd/v22/daemon)
[and the systemd notification protocol](https://www.freedesktop.org/software/systemd/man/257/sd_notify.html)

Do not select Type=notify without changing the program: systemd will wait for
the notification and then fail the start on TimeoutStartSec.

## 5. Journald logging

Use Go's standard log/slog with a JSON handler writing to os.Stderr:

~~~go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger)
~~~

log/slog is the standard library's structured logger: records have a message,
severity, and key/value attributes. JSON makes the MESSAGE content useful to
both journalctl and a later log collector, while stderr keeps the application
portable outside systemd.
[Go log/slog](https://pkg.go.dev/log/slog)
[and the Go structured-logging guidance](https://go.dev/blog/slog)

The unit explicitly connects both stdout and stderr to journald. systemd
normally connects service output to the journal, and journald splits stream
input into records at newlines, so emit one complete record per line and avoid
multiline stack traces where possible.
[systemd-journald.service(8)](https://www.freedesktop.org/software/systemd/man/257/systemd-journald.service.html)

Useful application attributes include request_id, method, path, status,
duration_ms, error, component, and version. Avoid passwords, session tokens,
cookies, and full request bodies. The unit's SyslogIdentifier=pushup-counter
gives the stream a stable identifier.

Do not try to emit or overwrite trusted underscore-prefixed fields such as
_SYSTEMD_USER_UNIT, _SYSTEMD_CGROUP, _PID, or _SELINUX_CONTEXT. journald
supplies those from the process and cgroup. In a user service they are the
fields that make journalctl --user -u pushup-counter.service and
journalctl --user _SYSTEMD_USER_UNIT=pushup-counter.service useful.
[systemd.journal-fields(7)](https://www.freedesktop.org/software/systemd/man/257/systemd.journal-fields.html)

If the application needs journal-native indexed fields rather than JSON inside
MESSAGE, use a native journal API and send non-trusted fields such as
PRIORITY=, MESSAGE_ID=, ERRNO=, and SYSLOG_IDENTIFIER= with correctly formatted
values. That is an additional integration choice; it is not needed for this
app. The pure-Go journal package is included in the go-systemd project.
[go-systemd journal integration](https://github.com/coreos/go-systemd#journal)

Inspect logs with:

~~~sh
journalctl --user -u pushup-counter.service --since today --no-pager
journalctl --user -u pushup-counter.service -f
journalctl --user -u pushup-counter.service -o json-pretty
~~~

## 6. Install, enable, start, and verify

Run these commands as the account that will own the user service. The install
command assumes the already-built static binary is in the current directory;
it does not build or modify the Go program.

~~~sh
install -D -m 0755 ./pushup-counter "$HOME/.local/bin/pushup-counter"
install -d -m 0700 "$HOME/data"
install -d -m 0700 "$HOME/.config/systemd/user"
~~~

Save the unit above as:

~~~
$HOME/.config/systemd/user/pushup-counter.service
~~~

Before starting, validate and load it:

~~~sh
systemd-analyze --user verify "$HOME/.config/systemd/user/pushup-counter.service"
systemctl --user daemon-reload
systemctl --user enable pushup-counter.service
systemctl --user start pushup-counter.service
~~~

The equivalent one-step activation after the reload is:

~~~sh
systemctl --user enable --now pushup-counter.service
~~~

enable and start are separate operations: enabling creates the dependency link
from default.target; starting launches the process now. daemon-reload is needed
after changing a unit file.
[systemctl(1), user mode and enable](https://www.freedesktop.org/software/systemd/man/257/systemctl.html)

Verify the manager, unit, process state, and listener:

~~~sh
systemctl --user is-enabled pushup-counter.service
systemctl --user is-active pushup-counter.service
systemctl --user status --no-pager pushup-counter.service
systemctl --user show pushup-counter.service \
    -p Type -p ExecStart -p WorkingDirectory -p Restart -p RestartUSec \
    -p KillSignal -p TimeoutStopUSec -p MainPID
ss -ltn '( sport = :5554 )'
journalctl --user -u pushup-counter.service -n 100 --no-pager
~~~

If the service is inactive or failed, inspect the first error in
systemctl --user status and the journal before changing hardening settings.
Common causes are a wrong ExecStart path, a missing ~/data directory, a port
already in use, or the binary expecting flags rather than the example
environment variables.

To make it survive logout and reboot, enable linger and ensure the unit is
enabled in the user manager:

~~~sh
sudo loginctl enable-linger "$USER"
systemctl --user enable pushup-counter.service
loginctl show-user "$USER" --property=Linger --value
~~~

After a reboot, verify from a login shell for the account (or another shell
with that account's XDG_RUNTIME_DIR and user bus environment):

~~~sh
systemctl --user is-active pushup-counter.service
systemctl --user status --no-pager pushup-counter.service
~~~

Linger is the boot/lifetime part; WantedBy=default.target plus
systemctl --user enable is the unit-start part. Neither Restart= nor
enable-linger replaces the other.

## 7. SELinux on AlmaLinux 10

AlmaLinux follows the RHEL-compatible policy model. On a normal RHEL/AlmaLinux
targeted installation, ordinary Linux users are mapped to unconfined_u by
default, which is subject to fewer SELinux restrictions than confined users.
That usually means a user-launched systemd --user service inherits an
unconfined user domain and can execute a user-owned binary and bind the
non-privileged port 5554. This is an expectation to verify, not a reason to
disable SELinux.
[RHEL 10, Using SELinux](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/pdf/using_selinux/Red_Hat_Enterprise_Linux-10-Using_SELinux-en-US.pdf)

Check the actual labels and domains:

~~~sh
getenforce
id -Z
ls -ldZ "$HOME" "$HOME/.local" "$HOME/.local/bin" "$HOME/data"
ls -lZ "$HOME/.local/bin/pushup-counter"
systemctl --user show pushup-counter.service -p MainPID
# Replace 12345 with the MainPID printed above.
ps -p 12345 -o pid,comm,label,args
~~~

Potential problems are:

* A confined account (user_t, staff_t, guest_t, or another site-specific
  mapping) may be prevented from executing content in a writable home
  directory. The relevant execution boolean is domain-specific (for example,
  user_exec_content or staff_exec_content); inspect it with
  getsebool -a | grep exec_content and change it only with an intentional
  policy decision.
* A copied binary or directory can have a wrong label such as default_t,
  unlabeled_t, or a label from its old location. Restore the default labels
  before inventing a policy:

  ~~~sh
  restorecon -RFv "$HOME/.local/bin" "$HOME/data"
  ~~~

  If a non-standard application directory must have a persistent custom label,
  use semanage fcontext followed by restorecon; chcon alone is not a persistent
  policy mapping.
  [RHEL SELinux labeling guidance](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/using_selinux/configuring-selinux-for-applications-and-services-with-non-standard-configurations_using-selinux)
* A home filesystem mounted with noexec will reject execution from ~/bin or
  ~/.local/bin even when the mode bits and SELinux labels look right. Check it
  with findmnt -no TARGET,OPTIONS "$HOME".
* A port label is not a universal port whitelist. The common command
  semanage port -a -t http_port_t -p tcp 5554 is for a domain such as httpd_t
  that is allowed to bind http_port_t; do not apply it blindly to an ordinary
  unconfined Go user process. If the process is intentionally confined in a
  custom web-service domain, label the port with the type that domain's policy
  expects, then apply the change:

  ~~~sh
  semanage port -l | grep -E '(^|[[:space:]])5554([[:space:]]|$)'
  sudo semanage port -a -t http_port_t -p tcp 5554
  ~~~

  Use the second command only when the chosen SELinux domain is actually
  httpd_t-like and the policy/type is appropriate. RHEL's non-standard-port
  procedure demonstrates this mechanism for confined httpd; it does not turn
  an arbitrary process into httpd_t.
  [RHEL non-standard port procedure](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/using_selinux/configuring-selinux-for-applications-and-services-with-non-standard-configurations_using-selinux)
* A confined domain or site policy may also deny writes to ~/data. Check the
  denial and the directory label before changing the policy.

When a start or bind fails under enforcing mode, look for the denial instead of
guessing:

~~~sh
sudo ausearch -m AVC,USER_AVC,SELINUX_ERR,USER_SELINUX_ERR -ts recent -i
sudo journalctl -b --grep='SELinux\|avc:.*denied' --no-pager
~~~

Fix the file context, port type, boolean, or a deliberately written local
policy as indicated by the denial. Do not jump straight to audit2allow:
first check DAC permissions, labels, the process domain, and the actual
filesystem/port policy.
[RHEL SELinux troubleshooting](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/using_selinux/troubleshooting-problems-related-to-selinux_using-selinux)

## Sources

Primary sources used for this deployment decision:

* [systemd 257 unit configuration](https://www.freedesktop.org/software/systemd/man/257/systemd.unit.html)
* [systemd 257 service units](https://www.freedesktop.org/software/systemd/man/257/systemd.service.html)
* [systemd 257 execution environment and sandboxing](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html)
* [systemd 257 kill behavior](https://www.freedesktop.org/software/systemd/man/257/systemd.kill.html)
* [systemd 257 socket units](https://www.freedesktop.org/software/systemd/man/257/systemd.socket.html)
* [systemd 257 loginctl](https://www.freedesktop.org/software/systemd/man/257/loginctl.html)
* [systemd 257 journald service](https://www.freedesktop.org/software/systemd/man/257/systemd-journald.service.html)
* [systemd 257 journal fields](https://www.freedesktop.org/software/systemd/man/257/systemd.journal-fields.html)
* [systemd 257 notification protocol](https://www.freedesktop.org/software/systemd/man/257/sd_notify.html)
* [Go net/http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
* [Go os/signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
* [Go log/slog](https://pkg.go.dev/log/slog)
* [coreos/go-systemd v22 daemon and activation packages](https://pkg.go.dev/github.com/coreos/go-systemd/v22)
* [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)
* [RHEL 10 Using SELinux](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/pdf/using_selinux/Red_Hat_Enterprise_Linux-10-Using_SELinux-en-US.pdf)
