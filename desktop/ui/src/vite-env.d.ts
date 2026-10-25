/// <reference types="vite/client" />

interface Window {
  secOpenRelease?: (url: string) => Promise<void>
  secOpenDownloadFolder?: () => Promise<void>
  secOpenDisclosure?: (url: string) => Promise<void>
}
