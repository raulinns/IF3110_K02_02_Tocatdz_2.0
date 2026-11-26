import { expect, type Browser, type Page } from '@playwright/test'
import { enterDatabase, openTab, rowWith } from './support'
import { openAs, type Role } from './real-support'

// Langkah-langkah alur workflow yang dipakai bersama oleh spec server sungguhan, dengan sesi per peran dari real-setup
export const database = 'demo'

export const history = (page: Page) => page.getByRole('table', { name: 'Riwayat eksekusi' })

// Setiap langkah memakai konteks baru dengan sesi perannya, dan tidak boleh ada kesalahan konsol, CSP, atau respons gagal sepanjang langkah
// Langkah yang memang menguji penolakan server menyebut respons yang diharapkan lewat expected, misalnya /^409 .*\/change-requests$/
export async function step(browser: Browser, role: Role, body: (page: Page) => Promise<void>, expected: RegExp[] = []) {
  const { page, context, problems } = await openAs(browser, role)
  try {
    await page.goto('/databases')
    await enterDatabase(page, database)
    await body(page)
    expect(problems.filter((problem) => !expected.some((pattern) => pattern.test(problem)))).toEqual([])
  } finally {
    await context.close()
  }
}

export async function openRequest(page: Page, title: string, kind: string, status: string) {
  await openTab(page, 'Review')
  await rowWith(page, title, kind, status).first().getByRole('link', { name: title }).click()
  await expect(page.getByRole('table', { name: 'Approver per group' })).toBeVisible()
}

// Satu peran bisa memimpin lebih dari satu group yang berhak, sehingga semua baris miliknya diputuskan satu per satu
export async function decideAll(page: Page) {
  const buttons = page.getByRole('button', { name: 'Putuskan' })
  await expect(buttons.first()).toBeVisible()
  for (let remaining = await buttons.count(); remaining > 0; remaining = await buttons.count()) {
    await buttons.first().click()
    const dialog = page.getByRole('dialog', { name: /Keputusan untuk group/ })
    await dialog.getByLabel('Catatan (opsional)').fill('Disetujui oleh uji otomatis')
    await dialog.getByRole('button', { name: 'Setujui' }).click()
    await expect(page.getByText(/Anda menyetujui atas nama group/)).toBeVisible()
    await expect(buttons).toHaveCount(remaining - 1)
  }
}

export const approveAs = (browser: Browser, role: Role, title: string, kind: string) =>
  step(browser, role, async (page) => {
    await openRequest(page, title, kind, 'Menunggu review')
    await decideAll(page)
  })

// Pengaju menjalankan permintaan yang sudah disetujui lalu menunggu job mencapai hasil akhirnya lewat polling
export async function executeAs(browser: Browser, title: string, kind: string, outcome = 'Berhasil') {
  await step(browser, 'editor', async (page) => {
    await openRequest(page, title, kind, 'Disetujui')
    await page.getByRole('button', { name: 'Jalankan', exact: true }).click()
    await page.getByRole('dialog', { name: 'Jalankan permintaan' }).getByRole('button', { name: 'Jalankan' }).click()
    const stepper = page.getByRole('list', { name: 'Tahapan job' })
    await expect(stepper).toBeVisible()
    await expect(stepper.getByRole('listitem').last()).toContainText(outcome, { timeout: 60_000 })
    await expect(page.getByRole('status').filter({ hasText: 'Pembaruan otomatis berhenti.' })).toBeVisible()
  })
}

// Permintaan disetujui oleh semua leader yang berhak lalu dijalankan editor
export async function approveAndExecute(browser: Browser, title: string, kind: string, outcome = 'Berhasil') {
  await approveAs(browser, 'pengamat', title, kind)
  await approveAs(browser, 'admin', title, kind)
  await executeAs(browser, title, kind, outcome)
}

// Rollback dan forward diajukan dari kartu posisi di halaman riwayat, lalu disetujui semua leader dan dijalankan editor
export async function changePosition(browser: Browser, title: string, action: 'Rollback' | 'Forward') {
  const lower = action.toLowerCase()
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    await page.getByRole('button', { name: action, exact: true }).click()
    await page.getByRole('dialog', { name: `Ajukan ${lower}` }).getByRole('button', { name: `Ajukan ${lower}` }).click()
    await expect(page.getByText(`Permintaan ${lower} dibuat dan menunggu approval.`)).toBeVisible()
  })
  await approveAndExecute(browser, title, action)
}
