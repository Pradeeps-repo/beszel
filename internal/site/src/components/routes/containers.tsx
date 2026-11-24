import { useLingui } from "@lingui/react/macro"
import { memo, useEffect, useMemo } from "react"
import ContainersTable from "@/components/containers-table/containers-table"
import { FooterRepoLink } from "@/components/footer-repo-link"
import { productName } from "@/components/logo"

export default memo(() => {
	const { t } = useLingui()

	useEffect(() => {
		document.title = `${t`All Containers`} / ${productName}`
	}, [t])

	return useMemo(
		() => (
			<>
				<ContainersTable />
				<FooterRepoLink />
			</>
		),
		[]
	)
})
