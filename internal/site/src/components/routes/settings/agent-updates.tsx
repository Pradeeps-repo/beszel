import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import { useEffect, useState } from "react"
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/use-toast"
import { pb } from "@/lib/api"
import { $agentLatest } from "@/lib/stores"

type Settings = {
	enabled: boolean
	target?: string
	percent: number
	canary?: string[]
	window_start?: string
	window_end?: string
	timezone?: string
	concurrency: number
}

type Progress = {
	id: string
	name: string
	state: string
	error?: string
	target?: string
	forced?: boolean
}

type Payload = {
	settings: Settings
	latest?: { version?: string; commit?: string }
	target?: string
	progress?: Progress[]
}

const empty: Settings = {
	enabled: false,
	percent: 10,
	window_start: "01:00",
	window_end: "05:00",
	concurrency: 5,
}

export default function AgentUpdateSettings() {
	const [settings, setSettings] = useState<Settings>(empty)
	const [latest, setLatest] = useState("")
	const [target, setTarget] = useState("")
	const [progress, setProgress] = useState<Progress[]>([])
	const [filterName, setFilterName] = useState("")
	const [filterStatus, setFilterStatus] = useState("up")
	const [behindOnly, setBehindOnly] = useState(true)
	const [confirm, setConfirm] = useState<null | { label: string; body: Record<string, unknown> }>(null)

	async function load() {
		const data = await pb.send<Payload>("/api/beszel/agent-updates", {})
		setSettings({ ...empty, ...data.settings, canary: data.settings?.canary ?? [] })
		setLatest(data.latest?.version ?? "")
		setTarget(data.target ?? "")
		setProgress(data.progress ?? [])
		if (data.latest?.version) {
			$agentLatest.set(data.latest.version)
		}
	}

	useEffect(() => {
		load().catch((error) => {
			console.error(error)
		})
		const timer = setInterval(() => {
			load().catch(() => {})
		}, 5000)
		return () => clearInterval(timer)
	}, [])

	async function save() {
		try {
			await pb.send("/api/beszel/agent-updates/settings", { method: "PUT", body: settings })
			toast({ title: t`Settings saved` })
			await load()
		} catch (error) {
			console.error(error)
			toast({ title: t`Failed to save settings`, variant: "destructive" })
		}
	}

	async function runConfirmed() {
		if (!confirm) return
		try {
			await pb.send("/api/beszel/agent-updates", { method: "POST", body: confirm.body })
			toast({ title: t`Update queued` })
			setConfirm(null)
			await load()
		} catch (error) {
			console.error(error)
			toast({ title: t`Could not queue the update`, variant: "destructive" })
		}
	}

	return (
		<div className="space-y-6">
			<div>
				<h3 className="text-xl font-medium mb-2">
					<Trans>Agent updates</Trans>
				</h3>
				<p className="text-sm text-muted-foreground">
					<Trans>
						Automatic updates are off until you enable them. Agents already in the field need one manual
						upgrade before they can take an update from the hub.
					</Trans>
				</p>
				<p className="text-sm mt-2">
					<Trans>Latest build</Trans>: <span className="font-mono">{latest || t`unknown`}</span>
					{target && target !== latest ? (
						<>
							{" "}
							· <Trans>Target</Trans>: <span className="font-mono">{target}</span>
						</>
					) : null}
				</p>
			</div>
			<div className="flex items-center justify-between gap-4">
				<Label htmlFor="agent-auto">
					<Trans>Automatic updates</Trans>
				</Label>
				<Switch
					id="agent-auto"
					checked={settings.enabled}
					onCheckedChange={(enabled) => setSettings({ ...settings, enabled })}
				/>
			</div>
			<div className="grid gap-2">
				<Label htmlFor="agent-target">
					<Trans>Pin a version</Trans>
				</Label>
				<Input
					id="agent-target"
					value={settings.target ?? ""}
					placeholder="0.21.2-bw.12+abcdef"
					onChange={(event) => setSettings({ ...settings, target: event.target.value })}
				/>
			</div>
			<div className="grid sm:grid-cols-2 gap-4">
				<div className="grid gap-2">
					<Label htmlFor="agent-percent">
						<Trans>Rollout percent</Trans>
					</Label>
					<Input
						id="agent-percent"
						type="number"
						min={0}
						max={100}
						value={settings.percent}
						onChange={(event) => setSettings({ ...settings, percent: Number(event.target.value) })}
					/>
				</div>
				<div className="grid gap-2">
					<Label htmlFor="agent-concurrency">
						<Trans>Downloads at once</Trans>
					</Label>
					<Input
						id="agent-concurrency"
						type="number"
						min={1}
						max={50}
						value={settings.concurrency}
						onChange={(event) => setSettings({ ...settings, concurrency: Number(event.target.value) })}
					/>
				</div>
			</div>
			<div className="grid gap-2">
				<Label htmlFor="agent-canary">
					<Trans>Canary systems</Trans>
				</Label>
				<Textarea
					id="agent-canary"
					value={(settings.canary ?? []).join("\n")}
					placeholder={"kiosk-a\nkiosk-b"}
					onChange={(event) =>
						setSettings({
							...settings,
							canary: event.target.value
								.split("\n")
								.map((item) => item.trim())
								.filter(Boolean),
						})
					}
				/>
				<p className="text-xs text-muted-foreground">
					<Trans>When this list is set, automatic updates ignore the percentage and use only these systems.</Trans>
				</p>
			</div>
			<div className="grid sm:grid-cols-3 gap-4">
				<div className="grid gap-2">
					<Label htmlFor="agent-start">
						<Trans>Window start</Trans>
					</Label>
					<Input
						id="agent-start"
						value={settings.window_start ?? ""}
						onChange={(event) => setSettings({ ...settings, window_start: event.target.value })}
					/>
				</div>
				<div className="grid gap-2">
					<Label htmlFor="agent-end">
						<Trans>Window end</Trans>
					</Label>
					<Input
						id="agent-end"
						value={settings.window_end ?? ""}
						onChange={(event) => setSettings({ ...settings, window_end: event.target.value })}
					/>
				</div>
				<div className="grid gap-2">
					<Label htmlFor="agent-tz">
						<Trans>Timezone</Trans>
					</Label>
					<Input
						id="agent-tz"
						placeholder="Europe/London"
						value={settings.timezone ?? ""}
						onChange={(event) => setSettings({ ...settings, timezone: event.target.value })}
					/>
				</div>
			</div>
			<Button type="button" onClick={() => void save()}>
				<Trans>Save</Trans>
			</Button>
			<div className="border-t pt-4 space-y-3">
				<h4 className="font-medium">
					<Trans>Force update</Trans>
				</h4>
				<p className="text-sm text-muted-foreground">
					<Trans>
						Force skips the window and the staged rollout. Downloads stay limited, and each agent still verifies
						sha256 and can roll back.
					</Trans>
				</p>
				<div className="grid sm:grid-cols-2 gap-3">
					<Input value={filterName} placeholder={t`Name contains`} onChange={(event) => setFilterName(event.target.value)} />
					<Input value={filterStatus} placeholder="up" onChange={(event) => setFilterStatus(event.target.value)} />
				</div>
				<label className="flex items-center gap-2 text-sm">
					<input type="checkbox" checked={behindOnly} onChange={(event) => setBehindOnly(event.target.checked)} />
					<Trans>Only systems behind the target</Trans>
				</label>
				<div className="flex flex-wrap gap-2">
					<Button
						type="button"
						variant="destructive"
						onClick={() =>
							setConfirm({
								label: t`Update every connected system now?`,
								body: { force: true, all: true },
							})
						}
					>
						<Trans>Force update all now</Trans>
					</Button>
					<Button
						type="button"
						variant="outline"
						onClick={() =>
							setConfirm({
								label: t`Update systems matching this filter?`,
								body: { force: true, filter: { name: filterName, status: filterStatus, behind: behindOnly } },
							})
						}
					>
						<Trans>Force matching systems</Trans>
					</Button>
				</div>
			</div>
			<div>
				<h4 className="font-medium mb-2">
					<Trans>Progress</Trans>
				</h4>
				{progress.length === 0 ? (
					<p className="text-sm text-muted-foreground">
						<Trans>No updates are in progress.</Trans>
					</p>
				) : (
					<ul className="text-sm space-y-1">
						{progress.map((item) => (
							<li key={item.id} className="flex flex-wrap gap-2">
								<span className="font-medium">{item.name}</span>
								<span className="uppercase text-muted-foreground">{item.state.replace("_", " ")}</span>
								{item.error ? <span className="text-destructive">{item.error}</span> : null}
							</li>
						))}
					</ul>
				)}
			</div>
			<AlertDialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>{confirm?.label}</AlertDialogTitle>
						<AlertDialogDescription>
							<Trans>
								Connected systems are queued. The concurrency limit still batches the downloads. Each agent
								checks sha256, keeps a backup, and rolls back if it does not come back healthy.
							</Trans>
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>
							<Trans>Cancel</Trans>
						</AlertDialogCancel>
						<AlertDialogAction onClick={() => void runConfirmed()}>
							<Trans>Update now</Trans>
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	)
}
