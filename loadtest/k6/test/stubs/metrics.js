const make = (type) =>
  class {
    constructor(name, isTime) {
      this.name = name
      this.type = type
      this.isTime = isTime === true
      this.values = []
      globalThis.__k6.metrics[name] = this
    }

    add(value, tags) {
      this.values.push({ value, tags })
    }
  }

export const Counter = make('counter')
export const Rate = make('rate')
export const Trend = make('trend')
