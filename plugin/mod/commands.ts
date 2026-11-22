// Answers /neoswitch:login, /neoswitch:status, /neoswitch:on and /neoswitch:off in Claude Code itself, without a model reply. As skills they end in a model turn that repeats the command's output: about 26 s on a session switched to Gemini, which /neoswitch:on spends on Gemini right after switching, a sign-in link a model may copy wrongly, notes a model may shorten, such as the one saying other windows still run an earlier NeoSwitch, and no answer at all while the switched session's model fails, when status matters most. Here the output shows at once, verbatim, and /neoswitch:login asks for the code in Claude Code's own dialog, which keeps it out of the conversation.
//
// The commands run with the same environment as the skills' commands: a spawned process gets Claude Code's own environment, which the project settings were applied to, under the variables of hookEnv.
//
// The skills stay: they register the commands, and they answer wherever this module does not, such as an older Claude Code, WSL or the desktop app over SSH. Before the command has run, any problem hands the command to the skill; after, it is answered here, because running the skill too would start a second sign-in and void the link the user may already have opened.
import type { EngineInterface, Register } from 'claude-code'

// The first Claude Code version whose command output can stay out of model requests (isTextDisplayOnly). cmd/neoswitch/claudeversion.go holds the same number for the hint the skill shows on older versions.
const MIN_VERSION = [2, 1, 326]

// The CLI ends its output in dialog mode with this line, naming the dialog to open (see dialogLine in cmd/neoswitch/signin.go).
const DIALOG_LINE = '@neoswitch-dialog '

// How the CLI reports the error it exits with; shown in the answer rather than as it comes, so it is not shown twice.
const ERROR_PREFIX = 'neoswitch: '

type Ran = { code: number | null; stdout: string; errors: string[] }

type Dialog = 'browser' | 'code' | 'again'

const CHOICES: Record<Dialog, { question: string; keep: string; cancel?: string }> = {
  browser: {
    question: 'Sign in in the browser that opened, or open the link above anywhere and paste the code the page shows under Type something.',
    keep: 'I signed in in the browser',
    cancel: 'Cancel this sign-in',
  },
  code: {
    question: 'Open the link above in any browser, sign in, and paste the code the page shows under Type something.',
    keep: 'Paste the code later',
    cancel: 'Cancel this sign-in',
  },
  again: {
    question: 'Open the link above, sign in with the other Google account, and paste the code the page shows under Type something.',
    keep: 'Paste the code later',
    cancel: 'Keep the current sign-in',
  },
}

// Claude Code's header keeps naming the Claude model a session asks for, so while this session's model requests go to Gemini a line under the prompt says which model answers. The daemon decides where a session's requests go and `neoswitch label` asks it, so the line is checked again after every NeoSwitch command and, while it shows or a sign-in may still finish in the browser, before each prompt: the session can also be switched from a terminal, the browser or another window's /neoswitch:on.
let labelled = false
let signingIn = false

export const register: Register = on => {
  on('prompt.submit', async ($, e, next) => {
    if (labelled || signingIn) void refreshLabel($)
    return next(e)
  })

  on('command.run', { command: 'neoswitch:login' }, async ($, e, next) => {
    if (!(await supported($))) return next(e)
    let started = false
    try {
      const env = await commandEnv($)
      const project = env.CLAUDE_PROJECT_DIR ?? ''
      const arg = e.args.trim()
      started = true
      if (arg !== '' && arg !== 'cancel') {
        // The code goes on stdin, not the command line, where other programs on the computer could read it.
        $.ui.log('Checking the code…')
        const checked = answer(await run($, env, ['antigravity', 'login', '--on', project, '-'], arg))
        void refreshLabel($)
        return checked
      }
      const ran = await run($, env, ['antigravity', 'login', '--on', project, ...(arg === '' ? [] : [arg])])
      const { text, dialog } = splitDialog(ran.stdout)
      if (dialog !== undefined) {
        // Asked after the answer, so the link above is shown before the dialog covers the prompt.
        signingIn = true
        void askForCode($, env, project, dialog, linkIn(text)).then(() => refreshLabel($))
      }
      void refreshLabel($)
      return answer({ ...ran, stdout: text })
    } catch (err) {
      if (!started) return next(e)
      return { text: `NeoSwitch: ${message(err)}. Run /neoswitch:login again.`, isTextDisplayOnly: true as const }
    }
  })

  for (const command of ['status', 'on', 'off'] as const) {
    on('command.run', { command: `neoswitch:${command}` }, async ($, e, next) => {
      if (!(await supported($))) return next(e)
      let started = false
      try {
        const env = await commandEnv($)
        started = true
        const shown = answer(await run($, env, [command, env.CLAUDE_PROJECT_DIR ?? '']), `/neoswitch:${command}`)
        void refreshLabel($)
        return shown
      } catch (err) {
        if (!started) return next(e)
        // After the command ran, the skill would run it a second time.
        const where = command === 'status' ? '' : ' Run /neoswitch:status to see where this session stands.'
        return { text: `NeoSwitch: ${message(err)}.${where}`, isTextDisplayOnly: true as const }
      }
    })
  }
}

// supported tells whether this Claude Code keeps the answer from the model; otherwise the skill answers. An engine too old to report its version throws here, which counts as too old.
async function supported($: EngineInterface): Promise<boolean> {
  try {
    const { version } = await $.session.version()
    const parts = /^(\d+)\.(\d+)\.(\d+)/.exec(version)
    if (parts === null) return false
    for (const [i, min] of MIN_VERSION.entries()) {
      const n = Number(parts[i + 1])
      if (n !== min) return n > min
    }
    return true
  } catch {
    return false
  }
}

// commandEnv is what the skill's command would run with: the session's variables, which tell the CLI which session to switch, and dialog mode.
async function commandEnv($: EngineInterface): Promise<Record<string, string>> {
  const env = await $.process.hookEnv()
  if (!env.CLAUDE_PLUGIN_ROOT || !env.CLAUDE_PROJECT_DIR) throw new Error('Claude Code did not name the plugin or project folder')
  return { ...env, NEOSWITCH_DIALOG: '1' }
}

// run runs the plugin's CLI, showing its progress and warnings as they come: the first run after an install or update builds or downloads NeoSwitch, and a first sign-in installs agy, either of which can take minutes.
async function run($: EngineInterface, env: Record<string, string>, args: string[], input?: string): Promise<Ran> {
  const stream = $.process.spawn({
    argv: [`${env.CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh`, ...args],
    cwd: env.CLAUDE_PROJECT_DIR,
    env,
    ...(input === undefined ? {} : { input }),
  })
  let stdout = ''
  let partial = ''
  const errors: string[] = []
  const show = (line: string) => {
    if (line.startsWith(ERROR_PREFIX)) errors.push(line.slice(ERROR_PREFIX.length))
    else if (line.trim() !== '') $.ui.log(line)
  }
  for await (const piece of stream) {
    if (piece.stream === 'stdout') {
      stdout += piece.text
      continue
    }
    const lines = (partial + piece.text).split('\n')
    partial = lines.pop() ?? ''
    lines.forEach(show)
  }
  show(partial)
  const { code } = await stream.result
  return { code, stdout, errors }
}

// answer shows what the CLI said; command is the one to run again when it stopped without saying why.
function answer(ran: Ran, command = '/neoswitch:login'): { text: string; isTextDisplayOnly: true } {
  const parts = [ran.stdout.trim(), ...ran.errors].filter(part => part !== '')
  if (parts.length === 0 && ran.code !== 0) parts.push(`NeoSwitch stopped without saying why. Run ${command} again.`)
  return { text: parts.join('\n'), isTextDisplayOnly: true }
}

function splitDialog(stdout: string): { text: string; dialog?: Dialog } {
  const lines = stdout.replace(/\n+$/, '').split('\n')
  const last = lines[lines.length - 1] ?? ''
  if (!last.startsWith(DIALOG_LINE)) return { text: stdout }
  const dialog = last.slice(DIALOG_LINE.length).trim()
  if (dialog !== 'browser' && dialog !== 'code' && dialog !== 'again') return { text: lines.slice(0, -1).join('\n') }
  return { text: lines.slice(0, -1).join('\n'), dialog }
}

function linkIn(text: string): string {
  return text.split('\n').find(line => line.startsWith('https://')) ?? ''
}

// askForCode finishes the sign-in from the dialog. Closing the dialog leaves the sign-in waiting: the browser can still finish it, and /neoswitch:login followed by the code still does.
async function askForCode($: EngineInterface, env: Record<string, string>, project: string, dialog: Dialog, link: string): Promise<void> {
  const choices = CHOICES[dialog]
  const later = () => $.ui.log('The link works for 10 minutes. To finish later, run /neoswitch:login followed by the code the page shows.')
  try {
    let picked: string
    try {
      picked = await $.ui.ask(choices.question, [choices.keep, ...(choices.cancel === undefined ? [] : [choices.cancel])])
    } catch {
      later()
      return
    }
    picked = picked.trim()
    if (picked === choices.cancel) {
      logAnswer($, await run($, env, ['antigravity', 'login', 'cancel']))
    } else if (picked === choices.keep && dialog === 'browser') {
      logAnswer($, await run($, env, ['status', project]))
    } else if (picked === choices.keep || picked === '') {
      later()
    } else {
      $.ui.log('Checking the code…')
      // The link shown lets the CLI refuse a code for a sign-in another /neoswitch:login replaced meanwhile, before agy spends it.
      logAnswer($, await run($, { ...env, NEOSWITCH_SIGN_IN_LINK: link }, ['antigravity', 'login', '--on', project, '-'], picked))
    }
  } catch (err) {
    $.ui.log(`NeoSwitch: ${message(err)}. Run /neoswitch:login again.`)
  }
}

function logAnswer($: EngineInterface, ran: Ran): void {
  for (const line of answer(ran).text.split('\n')) {
    if (line.trim() !== '') $.ui.log(line)
  }
}

function message(err: unknown): string {
  return (err instanceof Error ? err.message : String(err)).replace(/\.$/, '')
}

// refreshLabel pins the line `neoswitch label` prints, or removes it when it prints none. It runs the binary already built (--cached), so it never builds or downloads, and any problem leaves the line as it was.
async function refreshLabel($: EngineInterface): Promise<void> {
  try {
    const env = await commandEnv($)
    const ran = await $.process.run([`${env.CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh`, '--cached', 'label'], { cwd: env.CLAUDE_PROJECT_DIR, env, timeoutMs: 10_000 })
    const line = ran.stdout.trim()
    // A binary from before the command prints its usage and fails.
    if (ran.exitCode !== 0 || (line !== '' && (!line.startsWith('NeoSwitch: ') || line.includes('\n')))) return
    $.ui.status(line === '' ? undefined : line)
    labelled = line !== ''
  } catch {
    // The line stays as it was.
  }
}
