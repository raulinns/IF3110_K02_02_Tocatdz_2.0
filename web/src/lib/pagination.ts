export const positiveInteger = (value: string | null) => (value !== null && /^[1-9][0-9]{0,6}$/.test(value) ? Number(value) : undefined)

export const readPage = (params: URLSearchParams) => positiveInteger(params.get('page')) ?? 1

// Halaman pertama tidak ditulis ke URL supaya tautannya tetap pendek
export const pageSearch = (page: number) => (page > 1 ? `?page=${page}` : '')
