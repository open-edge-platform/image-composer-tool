// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react'

// One page of the in-app User Guide. Adding a page is adding an entry to
// guideTopics: the guide's sidebar and its Previous/Next buttons are both built
// from it, in this order.
export interface GuideTopic {
  id: string
  title: string
  // One line shown under the page title.
  summary: string
  body: ReactNode
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mb-8">
      <h2 className="mb-3 text-lg font-semibold text-[#00285a]">{title}</h2>
      {children}
    </section>
  )
}

function P({ children }: { children: ReactNode }) {
  return <p className="mb-3 text-sm leading-6 text-slate-700">{children}</p>
}

function Steps({ children }: { children: ReactNode }) {
  return <ol className="list-decimal space-y-2 pl-6 text-sm leading-6 text-slate-700">{children}</ol>
}

// Ui names a button, tab or field exactly as it is labelled on screen, so the
// user can match the instruction to what they see.
function Ui({ children }: { children: ReactNode }) {
  return <span className="font-semibold text-[#00285a]">{children}</span>
}

function Tip({ children }: { children: ReactNode }) {
  return (
    <div className="my-3 rounded border border-blue-200 bg-blue-50 p-3 text-sm text-[#00285a]">
      <span className="font-semibold">Tip: </span>
      {children}
    </div>
  )
}

// Card is one tile of the Welcome page's "what each tab does" overview.
function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <p className="mb-1 font-semibold text-[#00285a]">{title}</p>
      <p className="text-sm text-slate-600">{children}</p>
    </div>
  )
}

const welcome: GuideTopic = {
  id: 'welcome',
  title: 'Welcome',
  summary: 'What Image Composer Tool does, and how to compose your first image.',
  body: (
    <>
      <P>
        Image Composer Tool (ICT) builds a ready-to-boot Linux image for Intel platforms. You pick
        what the image is for, and ICT puts together the operating system, packages and settings
        into a single file you can flash to a device or run in a virtual machine.
      </P>
      <P>Building an image is called <Ui>composing</Ui>. You do not need to write any configuration
        files: everything is done with the menus on screen.</P>

      <Section title="The three tabs">
        <div className="grid gap-3 md:grid-cols-3">
          <Card title="Basic">
            The quickest way. Choose a use case, SKU, platform and operating system, then compose.
          </Card>
          <Card title="Advanced">
            Start from the same choices, then add packages and change the disk layout before you
            compose.
          </Card>
          <Card title="Compose Image">
            Watch your image being built, read the log, download the finished image, and look back
            at earlier builds.
          </Card>
        </div>
      </Section>

      <Section title="Your first image in 4 steps">
        <Steps>
          <li>
            On the <Ui>Basic</Ui> tab, choose a <Ui>Use Case</Ui>, <Ui>SKU</Ui>, <Ui>Platform</Ui>{' '}
            and <Ui>Operating System</Ui>.
          </li>
          <li>
            Click <Ui>Compose Image</Ui>.
          </li>
          <li>Set a password for the image's user, then click <Ui>Compose Image</Ui> again.</li>
          <li>
            Wait on the <Ui>Compose Image</Ui> tab until it says the compose completed, then click
            your image's name in the <Ui>Artifacts</Ui> table to download it.
          </li>
        </Steps>
        <Tip>
          The <Ui>?</Ui> (Help) button at the top right always brings you back to this guide.
        </Tip>
      </Section>
    </>
  ),
}

const basic: GuideTopic = {
  id: 'basic',
  title: 'Compose with Basic',
  summary: 'The fastest way to a working image: four choices and one button.',
  body: (
    <>
      <Section title="Steps">
        <Steps>
          <li>
            Click the <Ui>Basic</Ui> tab at the top.
          </li>
          <li>
            Choose a <Ui>Use Case</Ui>. This is what the image is for. The other lists open once
            you have chosen it.
          </li>
          <li>
            Choose a <Ui>SKU</Ui>, then a <Ui>Platform</Ui> (your Intel hardware), then an{' '}
            <Ui>Operating System</Ui>.
          </li>
          <li>
            Check the <Ui>Image Configuration</Ui> summary on the right. It shows exactly what will
            be built.
          </li>
          <li>
            Click <Ui>Compose Image</Ui>. The <Ui>Users and credentials</Ui> window opens.
          </li>
          <li>
            Type a <Ui>Password</Ui> for each user listed, or paste an <Ui>SSH public key</Ui>.
            One of the two is enough.
          </li>
          <li>
            Click <Ui>Compose Image</Ui> in that window. ICT switches to the{' '}
            <Ui>Compose Image</Ui> tab so you can follow the build.
          </li>
        </Steps>
        <Tip>
          The <Ui>Compose Image</Ui> button stays greyed out until all four lists are filled in.
          It is also greyed out while another image is being built, because ICT builds one image at
          a time. The message next to it tells you which.
        </Tip>
        <Tip>
          A choice marked <Ui>(coming soon)</Ui> is planned but cannot be built yet.
        </Tip>
        <Tip>
          Write the password down. You need it to log in to the image, and ICT does not show it
          again.
        </Tip>
      </Section>

      <Section title="Add your own user (optional)">
        <P>
          In the <Ui>Users and credentials</Ui> window, click <Ui>+ Add user</Ui>. Type a{' '}
          <Ui>User name</Ui> and a password or SSH key, and tick <Ui>Administrator (sudo)</Ui> if
          the user needs admin rights.
        </P>
      </Section>

      <Section title="Need more than the defaults?">
        <P>
          Click <Ui>Edit in Advanced</Ui>. Your choices are carried over to the <Ui>Advanced</Ui>{' '}
          tab, where you can add packages or change the disk layout.
        </P>
      </Section>
    </>
  ),
}

const advanced: GuideTopic = {
  id: 'advanced',
  title: 'Customize with Advanced',
  summary: 'Add packages and change the disk layout, one step at a time.',
  body: (
    <>
      <P>
        The <Ui>Advanced</Ui> tab walks you through four steps, shown as numbered circles at the
        top. Use <Ui>Next</Ui> and <Ui>Back</Ui> to move between them, or click a circle to jump
        to that step. A finished step turns green with a tick, and a step with a problem to fix
        turns red with a <Ui>!</Ui>.
      </P>

      <Section title="Step 1: Target">
        <P>
          Choose the <Ui>Use Case</Ui>, <Ui>SKU</Ui>, <Ui>Platform</Ui> and{' '}
          <Ui>Operating System</Ui>, the same way as on the Basic tab. If you came from{' '}
          <Ui>Edit in Advanced</Ui>, these are already filled in. You can also change the{' '}
          <Ui>Image Name</Ui>. <Ui>Next</Ui> stays greyed out until every required field is
          filled in, and the message beside it says which are missing.
        </P>
      </Section>

      <Section title="Step 2: Packages">
        <Steps>
          <li>
            To find a package by name, type it in the search box at the top.
          </li>
          <li>
            Or browse: the <Ui>Edge Packs</Ui> tab lists ready-made Intel software bundles, and the{' '}
            <Ui>Repositories</Ui> tab shows the packages of the repository you pick from{' '}
            <Ui>Choose repository</Ui>. Repositories the template already uses are listed first.
          </li>
          <li>
            Tick a package, or pick a version, to add it. A repository you add a package from is
            used by the build for as long as one of your packages comes from it.
          </li>
          <li>
            Everything you added or changed shows in the <Ui>Selected</Ui> panel on the right, under{' '}
            <Ui>You are adding</Ui>. Packages the image already has show ticked in the{' '}
            <Ui>Repositories</Ui> tab. Tick <Ui>Show only selected</Ui> there to list just the
            packages selected from the repository you are browsing.
          </li>
        </Steps>
        <Tip>
          Leave a package unpinned (ticked, no version chosen) unless you need one exact version;
          choose a version chip to pin it, and choose it again to unpin it. An unpinned package is
          looked up when the image is built, so a later build can get a newer version. To fix each
          package selected from a repository, added or from the template, to the newest version that
          repository offers today instead, browse that repository and click{' '}
          <Ui>Update to latest version</Ui> above the package list in the <Ui>Repositories</Ui> tab.
        </Tip>
      </Section>

      <Section title="Step 3: Disk">
        <P>
          Set the disk size, partition table and partitions. The defaults suit most devices, so
          you can usually just click <Ui>Next</Ui>.
        </P>
      </Section>

      <Section title="Step 4: Review">
        <Steps>
          <li>
            Read the summary, and the <Ui>Advanced customization</Ui> card, which lists what you
            changed.
          </li>
          <li>
            Optionally, click <Ui>Export YAML</Ui> to save your changes as a template file that
            extends the original one.
          </li>
          <li>
            Click <Ui>Compose Image</Ui>, set a password for each user (the same window as on the
            Basic tab), and click <Ui>Compose Image</Ui> again.
          </li>
        </Steps>
      </Section>
    </>
  ),
}

const compose: GuideTopic = {
  id: 'compose',
  title: 'Follow and download a compose',
  summary: 'Watch the build, get the finished image, and find earlier builds.',
  body: (
    <>
      <Section title="While it builds">
        <P>
          The <Ui>Compose Image</Ui> tab shows the build's progress and its log as it runs. Use the
          buttons at the top right of the log to copy it, or to download it once the build ends.
          To stop the build, click <Ui>Cancel compose</Ui>.
        </P>
        <P>
          ICT builds one image at a time. Until this build ends, the <Ui>Compose Image</Ui>{' '}
          buttons on the other tabs are greyed out.
        </P>
      </Section>

      <Section title="When it finishes">
        <Steps>
          <li>
            The <Ui>Artifacts</Ui> table lists the image and its bill of materials. Click a name to
            download it. Hover over a name to see where the file is on the server.
          </li>
          <li>
            Open <Ui>Compose details</Ui> to see what was built, and to download the{' '}
            <Ui>Template</Ui> the build used.
          </li>
        </Steps>
        <Tip>
          If a build fails or you cancel it, click <Ui>Retry compose</Ui> to run it again with the
          same choices and logins.
        </Tip>
      </Section>

      <Section title="Earlier builds">
        <P>
          The <Ui>History</Ui> list on the left shows every build, newest first. Click one to see
          its log and download its files. Use <Ui>Older</Ui> and <Ui>Newer</Ui> to page through
          the list.
        </P>
      </Section>
    </>
  ),
}

export const guideTopics: GuideTopic[] = [welcome, basic, advanced, compose]

// The guide lives at its own address (#guide/<topic>), so every way into it is
// an ordinary link: it can be bookmarked, opened in a new tab, and left with
// the browser's Back button.
const guidePrefix = '#guide/'

export const guideHref = (topicId: string) => guidePrefix + topicId

// guideTopicFromHash returns the topic a location hash points at, '' for the
// bare guide address, or null when the hash is not a guide address at all.
export const guideTopicFromHash = (hash: string) =>
  hash.startsWith(guidePrefix) ? hash.slice(guidePrefix.length) : null
