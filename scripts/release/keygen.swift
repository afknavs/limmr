#!/usr/bin/env swift
import CryptoKit
import Foundation
import Security

// No private material is ever printed. --file is for OFFLINE throwaway-key tests only.
// Real release keys live as a non-synchronizing, this-Mac-only Keychain generic password.
do {
  let key = Curve25519.Signing.PrivateKey()
  if CommandLine.arguments.count == 3 && CommandLine.arguments[1] == "--file" {
    let path = CommandLine.arguments[2]
    let fd = open(path, O_WRONLY | O_CREAT | O_EXCL, S_IRUSR | S_IWUSR)
    guard fd >= 0 else { throw NSError(domain: "keygen", code: 1) }
    let file = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    try file.write(contentsOf: key.rawRepresentation)
    try file.close()
  } else {
    guard CommandLine.arguments.count == 1 else { throw NSError(domain: "keygen", code: 2) }
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: "dev.limmr.release.ed25519",
      kSecAttrAccount as String: "signing",
      kSecAttrAccessible as String: kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
      kSecAttrSynchronizable as String: false,
      kSecValueData as String: key.rawRepresentation,
    ]
    guard SecItemAdd(query as CFDictionary, nil) == errSecSuccess else {
      // In particular: never overwrite an existing release key.
      throw NSError(domain: "keygen", code: 3)
    }
  }
  print(key.publicKey.rawRepresentation.base64EncodedString())
} catch {
  fputs("Key generation failed; an existing key was not overwritten.\n", stderr)
  exit(1)
}
