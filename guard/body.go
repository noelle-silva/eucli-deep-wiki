package guard

import (
	"bytes"
	"strconv"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// BodyChecks 守「正文与响应头一族」：正文是什么形态、信息放在头里还是正文里、
// 下载类接口的头怎么写。它们是「看起来像契约、实际不是」的高发区。
func BodyChecks() []Check {
	return []Check{
		{
			ID: "body.file.plaintext", Family: contract.FamilyBodies,
			Title: "文件接口的正文是纯文本，切段信息只走响应头",
			World: harness.KindStandard,
			Run:   checkFilePlaintext,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/file", Side: contract.Server},
			},
		},
		{
			ID: "body.file.truncated", Family: contract.FamilyBodies,
			Title: "按字节截断只截到完整行，并用固定标记说明原因",
			World: harness.KindStandard,
			Run:   checkFileTruncated,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/file", Side: contract.Server},
			},
		},
		{
			ID: "body.archive.headers", Family: contract.FamilyBodies,
			Title: "快照包与合并打包的正文类型、附件头与参数校验",
			World: harness.KindStandard,
			Run:   checkArchiveHeaders,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/archive", Side: contract.Server},
				{Endpoint: "GET /v1/archive", Side: contract.Server},
				{Format: contract.FormatZip, Side: contract.Server},
				{Format: contract.FormatTarGz, Side: contract.Server},
			},
		},
		{
			ID: "body.bundle.headers", Family: contract.FamilyBodies,
			Title: "带历史的 bundle 用固定正文类型与附件名下发展",
			World: harness.KindGit,
			Run:   checkBundleHeaders,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/bundle", Side: contract.Server},
			},
		},
	}
}

// checkFilePlaintext 文件接口：正文是原文，事实在头里。
func checkFilePlaintext(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/file")
	path := endpoint.Route(contract.RepoOnDisk)
	message := world.Get(path, harness.Query("path", "README.md"))
	if message.Status != endpoint.Status {
		reporter.Mismatch(contract.Server, endpoint.ID, "HTTP 200", message.Describe())
		return
	}

	reporter.Equal(contract.Server, endpoint.ID+" 的正文类型", contract.FileContentType, message.ContentType())
	reporter.Equal(contract.Server, endpoint.ID+" 的正文", harness.ReadmeContent, string(message.Body))
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderRepoRevision, harness.AlphaMainRevision)
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderFileSize, itoa(len(harness.ReadmeContent)))
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderFileBytes, itoa(len(message.Body)))
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderTotalLines, itoa(harness.ReadmeLines))
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderFirstLine, "1")
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderLastLine, itoa(harness.ReadmeLines))
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderCacheControl, "no-cache")
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderETag, "")
	reporter.Header(contract.Server, endpoint.ID, message, contract.HeaderModified, "")
	if truncated := message.Header.Get(contract.HeaderTruncated); truncated != "" {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的截断标记", "不出现", truncated)
	}

	// 正文不能伪装成结构化响应：它不是 JSON 对象。
	if strings.HasPrefix(strings.TrimSpace(string(message.Body)), "{") {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的正文", "纯文本原文", "看起来像 JSON")
	}

	// 同一个 ETag 再来一次：304，且不再带正文。
	etag := message.Header.Get(contract.HeaderETag)
	if etag != "" {
		again := world.Do(harness.Call{
			Method: "GET", Path: path, Query: harness.Query("path", "README.md"),
			Headers: map[string]string{"If-None-Match": etag},
		})
		reporter.Equal(contract.Server, endpoint.ID+" 带 If-None-Match 的状态码", 304, again.Status)
		if len(again.Body) != 0 {
			reporter.Mismatch(contract.Server, endpoint.ID+" 304 的正文", "空", string(again.Body))
		}
	}
}

// checkFileTruncated 按字节截断：只截到完整行，标记固定是 bytes。
func checkFileTruncated(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/file")
	path := endpoint.Route(contract.RepoOnDisk)
	firstLine := firstLineOf(harness.ReadmeContent)

	// 上限比第一行还短：什么都不给，也不给半行残句。
	message := world.Get(path, harness.Query("path", "README.md", "maxBytes", itoa(len(firstLine)-1)))
	reporter.Equal(contract.Server, endpoint.ID+" 首行超限时的截断标记", contract.TruncatedByBytes, message.Header.Get(contract.HeaderTruncated))
	reporter.Equal(contract.Server, endpoint.ID+" 首行超限时的正文", "", string(message.Body))
	reporter.Equal(contract.Server, endpoint.ID+" 首行超限时的 X-File-Bytes", "0", message.Header.Get(contract.HeaderFileBytes))
	reporter.Equal(contract.Server, endpoint.ID+" 首行超限时的 X-File-Last-Line", "0", message.Header.Get(contract.HeaderLastLine))
	reporter.Equal(contract.Server, endpoint.ID+" 首行超限时的总行数", itoa(harness.ReadmeLines), message.Header.Get(contract.HeaderTotalLines))

	// 上限只够第一行多一点：只给第一行。
	message = world.Get(path, harness.Query("path", "README.md", "maxBytes", itoa(len(firstLine)+9)))
	reporter.Equal(contract.Server, endpoint.ID+" 截到首行时的正文", firstLine, string(message.Body))
	reporter.Equal(contract.Server, endpoint.ID+" 截到首行时的 X-File-Last-Line", "1", message.Header.Get(contract.HeaderLastLine))
	reporter.Equal(contract.Server, endpoint.ID+" 截到首行时的截断标记", contract.TruncatedByBytes, message.Header.Get(contract.HeaderTruncated))
	reporter.Equal(contract.Server, endpoint.ID+" 截到首行时的 X-File-Bytes", itoa(len(firstLine)), message.Header.Get(contract.HeaderFileBytes))
}

// checkArchiveHeaders 快照包：正文类型、附件名与参数校验。
func checkArchiveHeaders(world *harness.World, reporter *Reporter) {
	single := contract.MustEndpoint("GET /v1/repos/{name}/archive")
	zipMessage := world.Get(single.Route(contract.RepoOnDisk), nil)
	reporter.Equal(contract.Server, single.ID+" 的状态码", 200, zipMessage.Status)
	reporter.Equal(contract.Server, single.ID+" 的正文类型", contract.ArchiveContentTypes[contract.FormatZip], zipMessage.ContentType())
	if disposition := zipMessage.Header.Get("Content-Disposition"); !strings.HasPrefix(disposition, contract.ContentDispositionPrefix) {
		reporter.Mismatch(contract.Server, single.ID+" 的附件头", contract.ContentDispositionPrefix+"…", disposition)
	}
	if !bytes.HasPrefix(zipMessage.Body, []byte("PK")) {
		reporter.Mismatch(contract.Server, single.ID+" 的正文", "zip 字节流", "不是 zip")
	}

	tarMessage := world.Get(single.Route(contract.RepoOnDisk), harness.Query("format", contract.FormatTarGz))
	reporter.Equal(contract.Server, single.ID+"（tar.gz）的正文类型", contract.ArchiveContentTypes[contract.FormatTarGz], tarMessage.ContentType())
	if disposition := tarMessage.Header.Get("Content-Disposition"); !strings.Contains(disposition, ".tar.gz") {
		reporter.Mismatch(contract.Server, single.ID+"（tar.gz）的附件名", "以 .tar.gz 结尾", disposition)
	}

	multi := contract.MustEndpoint("GET /v1/archive")
	missing := world.Get(multi.Path, nil)
	reporter.Outcome("GET /v1/archive 不带 include", missing, 400, "include_required")

	badFormat := world.Get(multi.Path, harness.Query("include", contract.RepoOnDisk, "format", "rar"))
	reporter.Outcome("GET /v1/archive 格式不合法", badFormat, 400, "invalid_format")

	combined := world.Get(multi.Path, harness.Query("include", contract.RepoOnDisk))
	reporter.Equal(contract.Server, multi.ID+" 的状态码", 200, combined.Status)
	reporter.Equal(contract.Server, multi.ID+" 的正文类型", contract.ArchiveContentTypes[contract.FormatZip], combined.ContentType())
	if !bytes.HasPrefix(combined.Body, []byte("PK")) {
		reporter.Mismatch(contract.Server, multi.ID+" 的正文", "zip 字节流", "不是 zip")
	}
}

// checkBundleHeaders bundle 下载：正文类型与附件名。
func checkBundleHeaders(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/bundle")
	message := world.Get(endpoint.Route(contract.RepoGitFullHistory), nil)
	if message.Status != endpoint.Status {
		reporter.Mismatch(contract.Server, endpoint.ID+"（真实仓库）", "HTTP 200", message.Describe())
		return
	}
	reporter.Equal(contract.Server, endpoint.ID+" 的正文类型", "application/x-git-bundle", message.ContentType())
	disposition := message.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disposition, contract.ContentDispositionPrefix) || !strings.Contains(disposition, contract.RepoGitFullHistory+".bundle") {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的附件头", "attachment; filename=\"<仓库>.bundle\"", disposition)
	}
	if len(message.Body) == 0 {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的正文", "非空 bundle", "空")
	}
}

// firstLineOf 返回文本的第一行（含换行符）。
func firstLineOf(text string) string {
	if index := strings.Index(text, "\n"); index >= 0 {
		return text[:index+1]
	}
	return text
}

// itoa 把数字转成文本，避免在断言里散落格式化细节。
func itoa(value int) string {
	return strconv.Itoa(value)
}
