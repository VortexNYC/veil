declare module "tweetnacl" {
  interface BoxKeyPair { publicKey: Uint8Array; secretKey: Uint8Array }
  interface BoxKeyPairFn {
    (): BoxKeyPair;
    fromSecretKey(secretKey: Uint8Array): BoxKeyPair;
  }
  interface Box {
    keyPair: BoxKeyPairFn;
    open(msg: Uint8Array, nonce: Uint8Array, publicKey: Uint8Array, secretKey: Uint8Array): Uint8Array | null;
  }
  const box: Box;
  function setPRNG(fn: (x: Uint8Array, n: number) => void): void;
  export { box, setPRNG };
  export default { box, setPRNG };
}

declare module "tweetnacl-sealedbox-js" {
  export function seal(message: Uint8Array, recipientPublicKey: Uint8Array): Uint8Array;
  export function open(
    ciphertext: Uint8Array,
    recipientPublicKey: Uint8Array,
    recipientSecretKey: Uint8Array,
  ): Uint8Array | null;
}
