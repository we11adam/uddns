# Release signing

UDDNS release builds embed one or more minisign public keys. The self-updater
requires `checksums.txt.minisig` to verify with one of those keys before it
trusts `checksums.txt` or downloads a release archive.

## Initial setup

1. Generate an encrypted key pair on a trusted offline workstation:

   ```shell
   minisign -G -p uddns-minisign.pub -s uddns-minisign.key
   ```

2. Create a protected GitHub Actions environment named `release`. Require a
   reviewer and restrict deployments to protected release tags.

3. Configure these environment values:

   - Secret `UDDNS_MINISIGN_SECRET_KEY_B64`: the base64 encoding of the complete
     encrypted `uddns-minisign.key` file.
   - Secret `UDDNS_MINISIGN_PASSWORD`: the private-key password.
   - Variable `UDDNS_MINISIGN_PUBLIC_KEYS`: line 2 of `uddns-minisign.pub`.
     Multiple keys must be comma-separated without whitespace.

4. Keep the encrypted private key and its recovery material outside the
   repository and GitHub. The Actions secret is a deployment copy, not the
   backup.

The release workflow reconstructs the signer public key and refuses to publish
unless it appears in `UDDNS_MINISIGN_PUBLIC_KEYS`. GoReleaser embeds that list
in every binary and uploads `checksums.txt.minisig`.

## Rotation

Do not replace a key in one step:

1. Add the new public key to `UDDNS_MINISIGN_PUBLIC_KEYS`.
2. Publish a transition release signed by the old key. It embeds both keys.
3. Switch the signing secrets to the new private key.
4. Publish subsequent releases with the new key.
5. Remove the old public key only after versions that trust only the old key no
   longer need a direct update path.

The first signed release is a trust bootstrap: older clients do not verify its
minisign signature. Once a signed build is installed, all later self-updates
fail closed on missing or invalid signatures. Historical unsigned releases
cannot be installed through the signed build's self-update command.
