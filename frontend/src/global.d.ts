interface Window {
  go: {
    [key: string]: {
      [key: string]: {
        [key: string]: (...args: any[]) => Promise<any>
      }
    }
  }
  runtime: {
    EventsOn: (event: string, callback: (...args: any[]) => void) => void
    EventsOff: (event: string) => void
    EventsEmit: (event: string, ...args: any[]) => void
    [key: string]: any
  }
}
