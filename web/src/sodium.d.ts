declare module 'libsodium-wrappers-sumo' {
    const sodium: {
        ready: Promise<void>;
        crypto_pwhash_ALG_ARGON2ID13: number;
        randombytes_buf(n: number): Uint8Array;
        memzero(b: Uint8Array): void;
        memcmp(a: Uint8Array, b: Uint8Array): boolean;
        crypto_pwhash(n: number, p: Uint8Array, salt: Uint8Array, ops: number, mem: number, alg: number): Uint8Array;
        crypto_auth_hmacsha256(m: Uint8Array, k: Uint8Array): Uint8Array;
        crypto_aead_xchacha20poly1305_ietf_encrypt(p: Uint8Array, aad: Uint8Array, secret: null, nonce: Uint8Array, key: Uint8Array): Uint8Array;
        crypto_aead_xchacha20poly1305_ietf_decrypt(secret: null, c: Uint8Array, aad: Uint8Array, nonce: Uint8Array, key: Uint8Array): Uint8Array;
    };
    export default sodium;
}
