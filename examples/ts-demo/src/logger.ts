// A stand-in logger — calling it is an observable (log) side effect.
export const logger = {
  info(message: string): void {
    // eslint-disable-next-line no-console
    console.log(message)
  },
}
