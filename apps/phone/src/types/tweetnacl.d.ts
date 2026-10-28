declare module "tweetnacl" {
  interface BoxKeyPair { publicKey: Uint8Array; secretKey: Uint8Array }
  interface Box {
    keyPair(): BoxKeyPair;
    keyPair: {
      (): BoxKeyPair;
      fromSecretKey(secretKey: Uint8Array): BoxKeyPair;
    };
    open(msg: Uint8Array, nonce: Uint8Array, publicKey: Uint8Array, secretKey: Uint8Array): Uint8Array | null;
    seal(msg: Uint8Array, nonce: Uint8Array, publicKey: Uint8Array, secretKey: Uint8Array): Uint8Array;
  }
  const box: Box;
  function setPRNG(fn: (x: Uint8Array, n: number) => void): void;
  export { box, setPRNG };
  export default { box, setPRNG };
}
