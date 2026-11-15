import { screen } from '@testing-library/react'
import { mockApi } from './test/utils'

describe('main', () => {
  it('mounts the app into #root', async () => {
    mockApi({ 'GET /api/me': { status: 401, body: { error: { code: 'unauthorized', message: 'x' } } } })
    document.body.innerHTML = '<div id="root"></div>'
    await import('./main')
    expect(await screen.findByText('Войти как')).toBeInTheDocument()
  })
})
