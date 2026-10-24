/// <reference types="vite/client" />

interface Window {
  secOpenDisclosure?: (url: string) => Promise<void>
}
