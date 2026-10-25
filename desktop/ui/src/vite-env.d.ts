/// <reference types="vite/client" />

interface Window {
  secOpenDownloadFolder?: () => Promise<void>
  secOpenDisclosure?: (url: string) => Promise<void>
}
