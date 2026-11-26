const placeholder = '__CSP_NONCE__'

// Nonce diisi server pada setiap respons halaman, dan di dev server penandanya tidak diganti sehingga diabaikan
export function readNonce(doc: Document = document): string | undefined {
  const value = doc.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content.trim()
  return value && value !== placeholder ? value : undefined
}
