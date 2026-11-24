import { useEffect } from "react"
import SmartTable from "@/components/routes/system/smart-table"
import { FooterRepoLink } from "@/components/footer-repo-link"
import { productName } from "@/components/logo"

export default function Smart() {
	useEffect(() => {
		document.title = `S.M.A.R.T. / ${productName}`
	}, [])

	return (
		<>
			<SmartTable />
			<FooterRepoLink />
		</>
	)
}
