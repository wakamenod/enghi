# enghi の使い方

enghi は Wiki と GTD を1つにした、自分だけのためのローカルアプリです。データはすべて手元の SQLite にあり、外には出ません。

GTD 側の言葉づかいは [GTD 入門](/guide/gtd) で説明しています。用語に迷ったらそちらを先に読んでください。この文書は **enghi の画面で何をどう操作するか** を説明します。

## まず何をするか {#start}

最初にやることは1つだけです。**GTD の画面を開いて、頭の中にあることを片っ端から入力してください。** 分類も判断も要りません。1行1件で、思いつく限り入れます。

入力欄はどの画面にもあります。キーボードの `c` を押すと、今いる画面のままクイック入力が開きます。

30件でも50件でも構いません。入れ終わったら、それが **Inbox** です。次は Inbox を1件ずつ処理していきます。

## Inbox — 未処理の箱 {#inbox}

入力したものが最初に入る場所です。ここにある項目は、まだ何も判断されていません。

項目をクリックすると **Clarify 画面** が開きます。上から順に、1件ずつ処理してください。

**Inbox を空にするのは日課です。** 1日1回、数分でいいので上から処理してください。毎日きれいに空にできなくても構いませんが、**週に1度の Weekly Review では必ず空にします。** そこが最後の砦です。

## Clarify — Action か Reference か {#clarify-screen}

Inbox の項目を開くと、2つの枠が並んでいます。この項目を **Action として扱う** か、**Reference として扱う** かを選ぶための画面です。

**Action にする場合** — 左の枠に書きます。

- **Next Action** は動詞で始まる1つの動作に書き直します。「経費精算」ではなく「領収書をスキャンして経理に送る」。
- **状態** を選びます (下の表を参照)。
- **Project / Context / Area** は、あてはまるものがあれば選びます。空でも構いません。
- **Scheduled の日付** は「その日まで見たくない」ものに入れます。**Deadline** は動かせない期限にだけ入れます。両者は別物です。何となく締切を入れると、締切が意味を失います。

**Reference にする場合** — 右の枠でタイトルと本文を書いて「記事を作成」を押すと、**Wiki 記事になり**、元の項目は `filed`（資料化済み）として残ります。完了でも破棄でもない、第3の行き先です。作った記事と元の項目はリンクで繋がります。

**Action でも Reference でもないもの** は、下の「削除」で捨ててください。捨てることは失敗ではなく、Clarify の正当な結果の1つです。

### 状態の使い分け {#states}

Clarify 画面の「状態」で選べる値です。**ここの区別が enghi の中心** なので、迷ったらこの表に戻ってきてください。

<div class="dg-wrap"><svg class="dg" viewBox="0 0 830 370" role="img" aria-label="Inbox から出た項目の行き先"><defs><marker id="a3ja" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="head" d="M 0 0 L 8 4 L 0 8 z"/></marker></defs><rect class="box" x="20" y="158" width="150" height="44" rx="6"/><text class="mono" x="95.0" y="176.0" text-anchor="middle">inbox</text><text class="small" x="95.0" y="193.0" text-anchor="middle">未処理の項目</text><rect class="box" x="250" y="20" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="38.0" text-anchor="middle">next</text><text class="small" x="375.0" y="55.0" text-anchor="middle">今すぐ実行できる</text><path d="M 170 180 L 210 180 L 210 42 L 248 42" marker-end="url(#a3ja)"/><rect class="box" x="250" y="76" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="94.0" text-anchor="middle">later</text><text class="small" x="375.0" y="111.0" text-anchor="middle">後続。順番が来たら next にする</text><path d="M 170 180 L 210 180 L 210 98 L 248 98" marker-end="url(#a3ja)"/><rect class="box" x="250" y="132" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="150.0" text-anchor="middle">waiting</text><text class="small" x="375.0" y="167.0" text-anchor="middle">他人待ち</text><path d="M 170 180 L 210 180 L 210 154 L 248 154" marker-end="url(#a3ja)"/><rect class="box" x="250" y="188" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="206.0" text-anchor="middle">scheduled</text><text class="small" x="375.0" y="223.0" text-anchor="middle">日付が来たら next に出る</text><path d="M 170 180 L 210 180 L 210 210 L 248 210" marker-end="url(#a3ja)"/><rect class="box" x="250" y="244" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="262.0" text-anchor="middle">someday</text><text class="small" x="375.0" y="279.0" text-anchor="middle">やると決めていない</text><path d="M 170 180 L 210 180 L 210 266 L 248 266" marker-end="url(#a3ja)"/><rect class="box" x="250" y="300" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="318.0" text-anchor="middle">filed</text><text class="small" x="375.0" y="335.0" text-anchor="middle">Reference だった</text><path d="M 170 180 L 210 180 L 210 322 L 248 322" marker-end="url(#a3ja)"/><path d="M 500 42 L 545 42"/><path d="M 500 98 L 545 98"/><path d="M 500 154 L 545 154"/><path d="M 500 210 L 545 210"/><path d="M 500 266 L 545 266"/><path d="M 545 42 L 545 266"/><path d="M 545 154 L 608 154" marker-end="url(#a3ja)"/><rect class="end" x="610" y="132" width="200" height="44" rx="6"/><text class="mono end-t" x="710.0" y="158.5" text-anchor="middle">done / dropped</text><path d="M 500 322 L 608 322" marker-end="url(#a3ja)"/><rect class="box" x="610" y="300" width="200" height="44" rx="6"/><text class="small" x="710.0" y="326.5" text-anchor="middle">Wiki 記事になる</text></svg></div>

| 状態 | 意味 | 表示される場所 |
|---|---|---|
| `inbox` | 未処理。まだ判断していない | Inbox |
| `next` | 今すぐ実行できる単一の行動 | Next Actions |
| `later` | ある Project の後続の行動。まだ順番が来ていない | Project 詳細のみ |
| `waiting` | 人に頼んで、返事や作業を待っている | Waiting For |
| `scheduled` | 特定の日付が来るまで着手しない | Scheduled。日付が来ると Next Actions にも出る |
| `someday` | いつかやる / たぶんやる | Someday / Maybe |
| `filed` | 行動ではなく資料だった。記事にした | (リストには出ない) |
| `done` | 完了 | (リストには出ない) |
| `dropped` | 破棄 | (リストには出ない) |

**`later` と `someday` は違います。** `later` は「やると決めているが、今は順番ではない」もの。`someday` は「やると決めていない」ものです。前者は Project の中で待機し、後者は Weekly Review のたびに「まだやらないか?」と問われます。

## ダッシュボード — 今日の全体像 {#dashboard}

ダッシュボード (`g` `d`) の上段は GTD の現況です。Inbox の件数、今日やること、停滞している Project、委譲から1週間以上経った Waiting For が並びます。

- **期限切れ** が先頭に出ます。**締切** を過ぎたタスクで、何日過ぎたかも表示します。予定日を過ぎただけのタスクは期限切れではなく、「今日」に並びます。
- **今日** は、締切か予定日が今日以前のタスクです。
- **7 日以内の締切** は、締切が来る前に知らせるためのものです。明日から1週間先までの締切を近い順に並べ、残り日数を添えます。「今日」に出ているものと Someday のものは出ません。日数は `config.toml` の `deadline_warning_days` で変えられます。

## Next Actions — 実行するリスト {#next}

**enghi で日常的にいちばん開く画面です。** ここに並ぶのは、今すぐ物理的に実行できる行動だけです。予定日が到来した `scheduled` のタスクも自動的にここに現れます (状態を書き換える必要はありません)。

上部の Context で絞り込めます。`@電話` を選べば、電話でできることだけが並びます。

**この画面が「〇〇の件」のような曖昧な項目で埋まり始めたら、Clarify が雑になっています。** その項目を開いて、次の物理的な1動作に書き直してください。

## Waiting For — 他人待ち {#waiting}

人に頼んだものが並びます。**委譲してから何日経ったか** が各行に出ます。

自分の手は離れていますが、追いかける責任は残っています。Weekly Review で上から眺め、返事が来ていないものを催促してください。日数表示はそのための目印です。

## Scheduled (Tickler) — その日まで隠しておく {#scheduled}

予定日を持つタスクの一覧です。**予定日が来るまで Next Actions には現れません。**

「来月になったら考える」ものをここに置くことで、それまでの間、目に入らなくなります。忘れてよくなるのが利点です。

タスクをここへ移すとき（題名をクリック、または行で `s`）、日付の下の **繰り返し** で定期タスクにできます。N日／週／か月／年ごと、曜日指定、毎月の日付、毎年の月日から選べます。曜日や日付は選んだ日付に合わせて初期値が入ります。各回はその日までここで待ち、当日になると自動で Next Actions に出てきます。[定期タスク](#recurrence) も参照してください。

## Someday / Maybe — 今はやらないもの {#someday}

「やるかもしれないが、今はやらない」ものの置き場です。

**ここは Weekly Review で必ず見直してください。** 見直されない Someday はただのゴミ箱です。逆に、毎週目を通す前提があれば、迷ったものを安心してここに送れます。

## Projects {#projects}

2つ以上の行動を要する、望ましい結果です。詳しくは [GTD 入門の Project の節](/guide/gtd#project)。

- **タイトル** は短い識別名 (「オフィス移転」)。
- **Outcome** は、どうなれば終わりかを1文で (「新オフィスに移転完了し、業務が再開している」)。任意入力ですが、**書くと Next Action が決めやすくなります。** 未記入の Project には Weekly Review で印が付きます。
- **Project Support Material** に Wiki 記事を1枚だけ紐づけられます。資料や検討メモの置き場です。それ以外の関連記事は本文中に `[[記事名]]` と書けば繋がります。

### 停滞している Project の検出 {#stalled}

**enghi が自動でやってくれることの中で、いちばん価値があるのがこれです。**

進行中なのに `next` / `waiting` / `scheduled` のタスクが1つもない Project を「停滞」として検出し、ダッシュボードと Weekly Review に出します。

止まった Project は、放っておくと止まったことに気づかれないまま止まり続けます。ここに名前が出たら、次に取れる行動を1つ決めてください。決められないなら、それは Someday に落とすか、Outcome の定義からやり直す合図です。

### 再検討日 {#review-on}

Someday にした Project には **再検討日** を設定できます。その日が来ると、ダッシュボードと Weekly Review に「再検討期日を迎えた Someday」として現れます。

Someday を本当の意味で使えるようにするための仕掛けです。浮上する日を決めておけば、安心して沈めておけます。

<!--feature:areas-->
## Areas of Responsibility {#areas}

完了することのない、維持し続ける領域です (「経理」「健康」「採用」)。

Project や単発の行動を Area に紐づけておくと、**その領域で今なにが動いているか** を一覧できます。Project にするほどではない単発の行動は、Project を経由せず直接 Area に付けられます。

Area にも Wiki 記事を1枚メモとして紐づけられます。

<!--feature:contexts-->
## Contexts {#contexts}

GTD のトップ画面の右側で追加できます。`@電話` `@自宅` `@買い物` `@メール` あたりから始めれば十分です。

**最初から多く作らないでください。** 使ってみて、絞り込みたくなったときに足すほうがうまくいきます。

## Weekly Review — 週に1度の見直し {#review-screen}

**この画面が enghi の存在理由です。** 週に1度、1時間ほど取って、上から順にチェックしていきます。

ウィザード形式にはしていません。**判断に必要なデータがすべて同じ画面に並んでいます。** 停滞 Project、再検討期日を迎えた Someday、Inbox、先週完了したもの、今後2週間の予定と締切、Waiting For の経過日数、定期タスクの系列。別の画面へ移動しなくても、その場で処理できます。

チェック項目をクリックすると、対応するデータの位置へ飛びます。全部終えたら、気づいたことを書いて「完了にする」を押してください。記録が残ります。

**時間がない週でも、「Project のリストを見直す」と「Someday を見直す」の2つだけはやってください。** 他を削ってもこの2つを残せば、仕組みは死にません。

## 定期タスク {#recurrence}

移動ダイアログの Scheduled か Clarify 画面の「繰り返し」で設定すると、**完了したときに次の1件が自動で作られます。** 先回りしてまとめて作ることはしないので、リストが未消化の定期タスクで埋まることはありません。

| 記法 | 意味 | 例 |
|---|---|---|
| `+1w` | 前回の **予定日** から1週間後 | 固定の周期で回すもの |
| `.+2w` | **完了した日** から2週間後 | シーツの洗濯。やった日から数えたいもの |
| `++1w` | 予定日に加算し、今日より後になるまで進める | 長く放置したものを現在まで追いつかせる |
| `weekly:tue,fri` | 毎週火曜と金曜 | ゴミ出し |
| `monthly:25` | 毎月25日 | 経費精算 |
| `monthly:last` | 毎月末 | |
| `yearly:04-01` | 毎年4月1日 | |

**`+1w` と `.+2w` の違いが実用上いちばん効きます。** 曜日が決まっているものは前者、やった日から数えたいものは後者です。

選択式の入力は `++` 以外の記法をすべて作れます（JavaScript が無効なときは、Clarify 画面に記法を直接書く欄が出ます）。すでに `++` などの記法を持つタスクは「カスタム」と表示され、そのまま保たれます。「繰り返さない」を選ぶと記法は消えます。

生成された次の1件は必ず **予定日付き** で作られるので、その日が来るまで Next Actions には出てきません。

「今回は飛ばす」は Clarify 画面の「今回はスキップ」、系列そのものを終わらせるのは「定期タスク系列を終了」です。Weekly Review には定期タスク系列の一覧が出るので、**惰性で回り続けているだけのものを棚卸ししてください。**

## 作業ログ {#work-log}

タスクごとに、Clarify 画面の下に **作業ログ** があります。試したこと・分かったこと・決めたことを、時刻付きの記録として積み上げていく場所です。本文は記事と同じ Markdown で、`[[リンク]]`、コードブロック、画像の貼り付けやドロップも使えます。`Ctrl`/`⌘`+`Enter` で追加します。

**開始** と **中断** は、実際に手を動かしていた時間の印です。開始中のタスクには、タスクの一覧で「作業中」のバッジが付きます。一覧では `p` で開始と中断を切り替えられます。作業中は状態の一つではありません。タスクは Next Actions など元の場所にとどまり、完了や破棄をすればそのまま作業も終わります。

作業ログは検索の対象で、「ログ」のバッジ付きで出ます。同じタスクの記録が複数当たったときは、最もよく当たった1件だけが出ます。記録はあとから編集・削除できます。開始や中断を押し間違えたときは、その記録を削除すれば取り消せます。記録に編集履歴はなく、編集すると上書きされます。

## 作業記録 — その日にやったこと {#day}

**作業記録** (`g` `l`、またはダッシュボードの「今日の作業 →」) は、1日分の記録です。終えたことと手を付けたことを、その日に書いた作業ログと一緒に並べます。開くと今日が出ます。ほかの日は、右の月カレンダー、`[` と `]` (前の日 / 次の日)、`t` (今日へ戻る)、日付欄で選べます。記録のある日には点が付きます。

1日の中身は4つに分かれます。

- **完了** — その日に完了したタスクと、その時刻。あとで完了を取り消したタスクは出ません。
- **作業中** — その日の終わりに、開始したまま中断していなかったタスク。今日なら今作業中のもの、過去の日なら開始と中断の記録から割り出します。
- **手を付けた** — それ以外で、その日に作業ログを書いたタスク。
- **取りやめ** — その日に破棄したタスク。定期タスクで飛ばした回も含みます。最初は畳んであります。

「Markdown でコピー」で、その日の記録を Markdown としてコピーできます。日報やチャットに貼る用です。ブラウザがコピーに対応していない場所 (Emacs の中など) では、テキストが選択された状態で出るので、手でコピーしてください。同じ内容は `/api/day?date=YYYY-MM-DD` でも取れます (JSON、`&format=markdown` で Markdown)。Claude Code のスキルは、頼まれるとこれを読んで日報の下書きを作ります。

ダッシュボードには今作業中のタスクが並び、Weekly Review からは先週の各日の記録へ飛べます。

## Wiki との関係 {#wiki}

GTD と Wiki は独立しています。**GTD を一切使わなくても Wiki は完全に動きます** し、その逆も同じです。

繋がるのは次の3か所だけです。

- Inbox の項目を **Reference にする** と Wiki 記事になる
- Project と Area が、**それぞれ記事を1枚** 持てる
- 記事の本文に `[[記事名]]` と書くとリンクになる (まだ無い記事でも書けます)

## キーボード {#keys}

| キー | 動作 |
|---|---|
| `c` | クイック入力 (どの画面からでも Inbox に追加) |
| `/` | 検索窓へ |
| `g` `d` | ダッシュボード |
| `g` `i` | Inbox |
| `g` `n` | Next Actions |
| `g` `p` | Project |
| `g` `w` | 記事一覧 |
| `g` `l` | 作業記録 (今日) |
| `j` / `k` | 一覧を上下に移動 |
| `Enter` | 選択中の項目を開く。タスクなら移動先を選ぶ (題名のクリックも同じ) |
| `u` | 直前の移動を元に戻す (画面下に通知が出ている間) |
| `p` | 選択中のタスクの作業を開始 / 中断 |
| `e` | 記事を編集 |
| `[` / `]` | 作業記録で前の日 / 次の日 |
| `t` | 作業記録で今日へ戻る (タスクを選択中なら題名の変更) |

## 用語の対応 {#glossary}

GTD の本と enghi の画面で、呼び方が違うものの対応表です。

| GTD の用語 | enghi の画面 |
|---|---|
| Inbox / In-basket | Inbox |
| Next Actions | Next Actions |
| Waiting For | Waiting For |
| Calendar / Tickler | Scheduled |
| Someday/Maybe | Someday / Maybe |
| Projects | Project |
| Project Outcome | Outcome |
| Project Support Material | Project Support Material (Wiki 記事) |
| Contexts | Context |
| Areas of Responsibility | Areas |
| Reference Material | Reference にする → Wiki 記事 |
| Weekly Review | Weekly Review |
