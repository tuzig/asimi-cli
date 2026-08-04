Feature: Unix Socket Peer Credential Verification — Username Impersonation Guard
  As a daemon operator
  I want SetContext to verify the client's Username against the Unix socket peer credential (SO_PEERCRED/LOCAL_PEERCRED)
  So that no process with socket access can impersonate another user's identity

  Background:
    Given a daemon process listening on a Unix socket
    And a client process connecting over that Unix socket

  # --- Happy path: matching credentials ---

  Scenario: Client username matches the socket peer's OS user
    Given the client sends SetContext with Username "daonb"
    And the Unix peer credential identifies the peer as UID belonging to "daonb"
    When the daemon calls unixPeerUsername on the connection
    Then unixPeerUsername returns "daonb"
    And the daemon compares "daonb" == "daonb"
    And the SetContext handshake succeeds
    And the Court is created with Username "daonb"

  # --- Impersonation attempt: mismatched credentials ---

  Scenario: Client claims a different username than the socket peer
    Given the client sends SetContext with Username "root"
    And the Unix peer credential identifies the peer as UID belonging to "daonb"
    When the daemon calls unixPeerUsername on the connection
    Then unixPeerUsername returns "daonb"
    And the daemon compares "daonb" != "root"
    And the SetContext handshake fails with a wire error
    And the error message contains "username mismatch"
    And the error message quotes both "root" and "daonb"
    And the connection is not served further

  # --- Non-Unix connection: no peer credential available ---

  Scenario: Client connects over a non-Unix transport (e.g. TCP, net.Pipe)
    Given the client connection is not a *net.UnixConn (e.g. net.Pipe)
    When the daemon calls unixPeerUsername on the connection
    Then unixPeerUsername returns "" (empty string, cannot verify)
    And the daemon skips the username check
    And the SetContext handshake proceeds with the client-supplied Username

  # --- Platform without peer cred support ---

  Scenario: Platform does not support peer credential retrieval
    Given the platform is neither Linux nor macOS (peercred_other.go)
    When the daemon calls unixPeerUsername on a *net.UnixConn
    Then unixPeerUsername returns "" (empty string, unsupported platform)
    And the daemon skips the username check
    And the SetContext handshake proceeds with the client-supplied Username

  # --- Closed socket: File() fails gracefully ---

  Scenario: Socket connection is closed before peer credential extraction
    Given the client connection is a *net.UnixConn
    And the connection has been closed
    When the daemon calls unixPeerUsername on the closed connection
    Then uc.File() returns an error
    And unixPeerUsername returns "" (empty string)
    And the daemon skips the username check
    And the SetContext handshake proceeds with the client-supplied Username

  # --- Empty client username ---

  Scenario: Client sends empty username while socket peer is identifiable
    Given the client sends SetContext with Username ""
    And the Unix peer credential identifies the peer as UID belonging to "daonb"
    When the daemon calls unixPeerUsername on the connection
    Then unixPeerUsername returns "daonb"
    And the daemon compares "daonb" != "" (non-empty verified != empty client)
    And the SetContext handshake fails with a wire error
    And the error message contains "username mismatch"
    And the Court is NOT created

  # --- Peer credential matches the same username ---

  Scenario: Client sending different casing triggers mismatch
    Given the client sends SetContext with Username "Daonb"
    And the Unix peer credential identifies the peer as UID belonging to "daonb"
    When the daemon calls unixPeerUsername on the connection
    Then unixPeerUsername returns "daonb"
    And the daemon compares "daonb" != "Daonb" (case-sensitive)
    And the SetContext handshake fails with a wire error
    And the error message contains "username mismatch"

  # --- Restriction: verification only applies to SetContext ---

  Scenario: Ping RPC is unaffected by peer credential check
    Given a client connection that has not yet sent SetContext
    When the client sends a Ping request
    Then the Ping handler does not call unixPeerUsername
    And the Ping response returns Ok=true
    And the connection remains open for the handshake

  # --- Multiple SetContext calls ---

  Scenario: Repeated SetContext with matching username is idempotent
    Given the first SetContext handshake succeeded with Username "daonb"
    When the client sends SetContext again with Username "daonb"
    Then unixPeerUsername returns "daonb" on each call
    And the daemon verifies "daonb" == "daonb" on the second call
    And the second SetContext call succeeds
    And reconfigureModel is called but the Court is not recreated (already exists)

  # --- Repeated SetContext with mismatched username after first success ---

  Scenario: Repeated SetContext with different username is rejected
    Given the first SetContext handshake succeeded with Username "daonb"
    When the client sends SetContext again with Username "root"
    Then unixPeerUsername returns "daonb" (peer does not change)
    And the daemon compares "daonb" != "root"
    And the second SetContext call fails with a wire error
    And the error message contains "username mismatch"
    And the existing Court remains active (not destroyed)